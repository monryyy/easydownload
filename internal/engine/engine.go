// Package engine implements the download queue and segmented HTTP downloads.
package engine

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"EasyDownload/internal/config"
	"EasyDownload/internal/store"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type downloadState struct {
	*Download
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64 // increments each time a run is scheduled
}

// Engine coordinates downloads.
type Engine struct {
	cfg     config.Config
	store   *store.Store
	client  *http.Client
	limiter *rate.Limiter

	mu     sync.Mutex
	states map[string]*downloadState
	order  []string
	active int

	ctx    context.Context
	cancel context.CancelFunc
	emitFn func(name string, data interface{})
}

// New creates an engine, restores saved downloads and starts scheduling.
func New(cfg *config.Config, st *store.Store, emit func(string, interface{})) (*Engine, error) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   cfg.MaxConnectionsPerDownload + 2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	e := &Engine{
		cfg:    *cfg,
		store:  st,
		client: &http.Client{Transport: transport},
		states: make(map[string]*downloadState),
		emitFn: emit,
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.updateLimiter()
	if err := e.restore(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) emit(name string, data interface{}) {
	if e.emitFn != nil {
		e.emitFn(name, data)
	}
}

func (e *Engine) emitState(d Download) {
	e.emit(EventState, StateEvent{Download: d})
}

func (e *Engine) emitProgress(d Download) {
	e.emit(EventProgress, ProgressEvent{
		ID: d.ID, Name: d.Name, Downloaded: d.Downloaded, Total: d.TotalSize,
	})
}

func (e *Engine) emitComplete(d Download) {
	e.emit(EventComplete, CompleteEvent{ID: d.ID, Name: d.Name, Path: d.FinalPath})
}

func (e *Engine) emitError(d Download) {
	e.emit(EventError, ErrorEvent{ID: d.ID, Name: d.Name, Error: d.Error})
}

func (e *Engine) updateLimiter() {
	limit := e.cfg.GlobalSpeedLimit
	if limit <= 0 {
		e.limiter = nil
		return
	}
	burst := int64(e.cfg.ChunkSize * 2)
	if burst < 1<<20 {
		burst = 1 << 20
	}
	if e.limiter == nil {
		e.limiter = rate.NewLimiter(rate.Limit(limit), int(burst))
		return
	}
	e.limiter.SetLimit(rate.Limit(limit))
	e.limiter.SetBurst(int(burst))
}

func (e *Engine) snapshotConfig() config.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// SetConfig applies new settings and persists them.
func (e *Engine) SetConfig(cfg *config.Config) error {
	if err := config.Save(cfg); err != nil {
		return err
	}
	e.mu.Lock()
	e.cfg = *cfg
	e.mu.Unlock()
	e.updateLimiter()
	return nil
}

// Config returns the current settings.
func (e *Engine) Config() config.Config {
	return e.snapshotConfig()
}

func (e *Engine) restore() error {
	downloads, err := e.store.LoadAll()
	if err != nil {
		return err
	}
	for _, d := range downloads {
		if d.Status != StatusCompleted && d.PartPath != "" {
			if _, statErr := os.Stat(d.PartPath); statErr != nil {
				d.Downloaded = 0
				d.Segments = nil
				d.Probed = false
			}
		}
		if d.Status == StatusActive || d.Status == StatusQueued {
			d.Status = StatusQueued
		}
		d.Error = ""
		e.states[d.ID] = &downloadState{Download: d}
		e.order = append(e.order, d.ID)
		if err := e.store.Save(d); err != nil {
			return err
		}
	}
	sort.Slice(e.order, func(i, j int) bool {
		return e.states[e.order[i]].CreatedAt.Before(e.states[e.order[j]].CreatedAt)
	})
	e.schedule()
	return nil
}

// Add queues a new download.
func (e *Engine) Add(rawURL string, checksum *Checksum) (Download, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Download{}, errors.New("please enter a valid http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Download{}, errors.New("only http and https URLs are supported")
	}
	now := time.Now()
	d := &Download{
		ID:        uuid.NewString(),
		URL:       rawURL,
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
		Checksum:  checksum,
	}
	if err := e.store.Save(d); err != nil {
		return Download{}, err
	}
	e.mu.Lock()
	e.states[d.ID] = &downloadState{Download: d}
	e.order = append(e.order, d.ID)
	e.mu.Unlock()

	e.emitState(d.Clone())
	e.schedule()
	return d.Clone(), nil
}

// List returns a snapshot of all downloads, newest first.
func (e *Engine) List() []Download {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Download, 0, len(e.states))
	for i := len(e.order) - 1; i >= 0; i-- {
		ds := e.states[e.order[i]]
		ds.mu.Lock()
		out = append(out, ds.Download.Clone())
		ds.mu.Unlock()
	}
	return out
}

func (e *Engine) find(id string) *downloadState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.states[id]
}

// Pause stops an active or queued download while keeping partial data.
func (e *Engine) Pause(id string) error {
	ds := e.find(id)
	if ds == nil {
		return fmt.Errorf("unknown download: %s", id)
	}
	ds.mu.Lock()
	if ds.Status != StatusActive && ds.Status != StatusQueued {
		ds.mu.Unlock()
		return nil
	}
	ds.Status = StatusPaused
	ds.UpdatedAt = time.Now()
	snap := ds.Download.Clone()
	cancel := ds.cancel
	ds.mu.Unlock()

	if err := e.store.Save(&snap); err != nil {
		return err
	}
	e.emitState(snap)
	if cancel != nil {
		cancel()
	}
	return nil
}

// Resume restarts a paused, failed or canceled download.
func (e *Engine) Resume(id string) error {
	ds := e.find(id)
	if ds == nil {
		return fmt.Errorf("unknown download: %s", id)
	}
	ds.mu.Lock()
	if ds.Status == StatusActive || ds.Status == StatusQueued || ds.Status == StatusCompleted {
		ds.mu.Unlock()
		return nil
	}
	ds.Status = StatusQueued
	ds.Error = ""
	ds.UpdatedAt = time.Now()
	snap := ds.Download.Clone()
	ds.mu.Unlock()

	if err := e.store.Save(&snap); err != nil {
		return err
	}
	e.emitState(snap)
	e.schedule()
	return nil
}

// Cancel stops a download and discards its partial file.
func (e *Engine) Cancel(id string) error {
	ds := e.find(id)
	if ds == nil {
		return fmt.Errorf("unknown download: %s", id)
	}
	ds.mu.Lock()
	if ds.Status == StatusCompleted {
		ds.mu.Unlock()
		return nil
	}
	wasActive := ds.Status == StatusActive
	cancel := ds.cancel
	partPath := ds.PartPath
	ds.Status = StatusCanceled
	ds.Downloaded = 0
	ds.Segments = nil
	ds.Probed = false
	ds.Error = ""
	ds.UpdatedAt = time.Now()
	snap := ds.Download.Clone()
	ds.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// An active worker still holds the file open; its cleanup removes it.
	if !wasActive && partPath != "" {
		_ = os.Remove(partPath)
	}
	if err := e.store.Save(&snap); err != nil {
		return err
	}
	e.emitState(snap)
	return nil
}

// Remove deletes a download record and optionally the finished file.
func (e *Engine) Remove(id string, deleteFile bool) error {
	ds := e.find(id)
	if ds == nil {
		return nil
	}
	if err := e.Cancel(id); err != nil {
		return err
	}
	ds.mu.Lock()
	partPath := ds.PartPath
	finalPath := ds.FinalPath
	ds.mu.Unlock()

	if partPath != "" {
		_ = os.Remove(partPath)
	}
	if deleteFile && finalPath != "" {
		_ = os.Remove(finalPath)
	}
	if err := e.store.Delete(id); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.states, id)
	for i, candidate := range e.order {
		if candidate == id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
	e.mu.Unlock()
	return nil
}

func (e *Engine) schedule() {
	for {
		e.mu.Lock()
		if e.active >= e.cfg.MaxConcurrentDownloads {
			e.mu.Unlock()
			return
		}
		var next *downloadState
		for _, id := range e.order {
			ds := e.states[id]
			ds.mu.Lock()
			queued := ds.Status == StatusQueued
			ds.mu.Unlock()
			if queued {
				next = ds
				break
			}
		}
		if next == nil {
			e.mu.Unlock()
			return
		}
		// Claim the download while both locks are held so a second scheduler
		// pass cannot pick the same item before run() starts.
		next.mu.Lock()
		next.generation++
		gen := next.generation
		next.Status = StatusActive
		next.UpdatedAt = time.Now()
		next.Error = ""
		next.mu.Unlock()
		e.active++
		e.mu.Unlock()

		go e.run(next, gen)
	}
}

func (e *Engine) run(ds *downloadState, gen uint64) {
	ctx, cancel := context.WithCancel(e.ctx)
	ds.mu.Lock()
	ds.cancel = cancel
	ds.Status = StatusActive
	ds.Error = ""
	ds.UpdatedAt = time.Now()
	snap := ds.Download.Clone()
	ds.mu.Unlock()

	if err := e.store.Save(&snap); err != nil {
		// Persistence failure should not stop the transfer.
		fmt.Fprintln(os.Stderr, "state save failed:", err)
	}
	e.emitState(snap)

	err := e.runAttempts(ctx, ds)

	cancel()
	ds.mu.Lock()
	if ds.generation == gen {
		ds.cancel = nil
	}
	ds.mu.Unlock()
	e.finish(ds, gen, err)
}

func (e *Engine) runAttempts(ctx context.Context, ds *downloadState) error {
	cfg := e.snapshotConfig()
	var err error
	for attempt := 0; attempt < cfg.RetryMaxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoff(
				attempt-1,
				time.Duration(cfg.RetryInitialDelayMS)*time.Millisecond,
				time.Duration(cfg.RetryMaxDelayMS)*time.Millisecond,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		err = e.attemptDownload(ctx, ds, cfg)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !isRetryable(err) {
			return err
		}
	}
	return err
}

func (e *Engine) finish(ds *downloadState, gen uint64, err error) {
	e.mu.Lock()
	e.active--
	_, exists := e.states[ds.Download.ID]
	e.mu.Unlock()

	ds.mu.Lock()
	if ds.generation != gen {
		// A newer run took over; ignore this stale cleanup.
		ds.mu.Unlock()
		e.schedule()
		return
	}
	status := ds.Status
	partPath := ds.PartPath
	ds.UpdatedAt = time.Now()
	if err != nil && status == StatusActive {
		ds.Status = StatusFailed
		ds.Error = err.Error()
		status = StatusFailed
	}
	snap := ds.Download.Clone()
	ds.mu.Unlock()

	if !exists {
		e.schedule()
		return
	}
	if status == StatusCanceled {
		_ = os.Remove(partPath)
	}
	if err := e.store.Save(&snap); err != nil {
		fmt.Fprintln(os.Stderr, "state save failed:", err)
	}
	e.emitState(snap)
	if status == StatusFailed {
		e.emitError(snap)
	}
	e.schedule()
}

func (e *Engine) attemptDownload(ctx context.Context, ds *downloadState, cfg config.Config) error {
	ds.mu.Lock()
	rawURL := ds.URL
	probed := ds.Probed
	total := ds.TotalSize
	rangeOK := ds.RangeOK
	etag := ds.ETag
	finalPath := ds.FinalPath
	partPath := ds.PartPath
	checksum := ds.Checksum
	name := ds.Name
	downloaded := ds.Downloaded
	ds.mu.Unlock()

	if probed {
		// Revalidate on retry/resume so we never append to a changed file.
		probe, probeErr := Probe(ctx, e.client, rawURL)
		if probeErr == nil {
			if etag != "" && probe.ETag != "" && etag != probe.ETag {
				return fatalf("file changed on the server (ETag mismatch)")
			}
			if probe.TotalSize != total {
				return fatalf("file size changed on the server")
			}
		}
	} else {
		probe, probeErr := Probe(ctx, e.client, rawURL)
		if probeErr != nil {
			return probeErr
		}
		ds.mu.Lock()
		ds.Probed = true
		ds.TotalSize = probe.TotalSize
		ds.RangeOK = probe.RangeOK
		rangeOK = probe.RangeOK
		ds.ETag = probe.ETag
		ds.LastMod = probe.LastMod
		if ds.Name == "" {
			ds.Name = probe.Name
		}
		total = probe.TotalSize
		name = ds.Name
		ds.mu.Unlock()
	}

	if finalPath == "" {
		safeName := name
		if safeName == "" {
			safeName = "download"
		}
		finalPath = filepath.Join(cfg.DownloadDir, safeName)
		if _, statErr := os.Stat(finalPath); statErr == nil {
			ext := filepath.Ext(finalPath)
			base := strings.TrimSuffix(finalPath, ext)
			finalPath = fmt.Sprintf("%s (%d)%s", base, time.Now().Unix(), ext)
		}
		partPath = finalPath + ".part"
		ds.mu.Lock()
		ds.FinalPath = finalPath
		ds.PartPath = partPath
		ds.Name = filepath.Base(finalPath)
		name = ds.Name
		ds.mu.Unlock()
	}

	// Seed the UI now that size and filename are known; the first ticker
	// update is 500ms away.
	ds.mu.Lock()
	seed := ds.Download.Clone()
	ds.mu.Unlock()
	e.emitProgress(seed)

	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if info.Size() != total {
		if err := file.Truncate(total); err != nil {
			file.Close()
			return err
		}
		if downloaded > 0 {
			ds.mu.Lock()
			ds.Downloaded = 0
			ds.Segments = nil
			ds.mu.Unlock()
		}
	}

	ds.mu.Lock()
	if !rangeOK {
		// Without range support a partial file cannot be continued safely.
		ds.Downloaded = 0
		ds.Segments = []Segment{{Index: 0, Start: 0, End: total - 1}}
	} else if len(ds.Segments) == 0 {
		ds.Segments = splitSegments(total, cfg.MaxConnectionsPerDownload, cfg.MinSegmentSize)
	}
	segments := append([]Segment(nil), ds.Segments...)
	ds.mu.Unlock()

	tickCtx, stopTicker := context.WithCancel(ctx)
	defer stopTicker()
	go e.progressLoop(tickCtx, ds, cfg)

	g, groupCtx := errgroup.WithContext(ctx)
	for i := range segments {
		idx := i
		g.Go(func() error {
			return e.runSegment(groupCtx, ds, file, idx, cfg)
		})
	}
	runErr := g.Wait()

	if err := file.Sync(); err != nil && runErr == nil {
		runErr = err
	}
	if err := file.Close(); err != nil && runErr == nil {
		runErr = err
	}
	if runErr != nil {
		return runErr
	}

	ds.mu.Lock()
	finalDownloaded := ds.Download.Downloaded
	ds.mu.Unlock()
	if finalDownloaded != total {
		return fatalf("downloaded %d bytes but expected %d", finalDownloaded, total)
	}
	if checksum != nil {
		if err := verifyChecksum(partPath, checksum); err != nil {
			return err
		}
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		return err
	}

	ds.mu.Lock()
	ds.Status = StatusCompleted
	ds.UpdatedAt = time.Now()
	snap := ds.Download.Clone()
	ds.mu.Unlock()
	if err := e.store.Save(&snap); err != nil {
		fmt.Fprintln(os.Stderr, "state save failed:", err)
	}
	e.emitState(snap)
	e.emitComplete(snap)
	return nil
}

func (e *Engine) progressLoop(ctx context.Context, ds *downloadState, cfg config.Config) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastBytes int64
	var speed float64
	lastTime := time.Now()
	tick := 0

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			ds.mu.Lock()
			cur := ds.Download.Downloaded
			total := ds.Download.TotalSize
			id := ds.Download.ID
			name := ds.Download.Name
			snap := ds.Download.Clone()
			ds.mu.Unlock()

			dt := now.Sub(lastTime).Seconds()
			if dt > 0 {
				sample := float64(cur-lastBytes) / dt
				if sample < 0 {
					sample = 0
				}
				if speed == 0 {
					speed = sample
				} else {
					speed = 0.7*speed + 0.3*sample
				}
			}
			lastBytes = cur
			lastTime = now

			var eta int64 = -1
			if speed > 0 && total > cur {
				eta = int64(float64(total-cur) / speed)
			}
			e.emit(EventProgress, ProgressEvent{
				ID: id, Name: name, Downloaded: cur, Total: total, Speed: speed, ETA: eta,
			})

			tick++
			if tick%2 == 0 {
				if err := e.store.Save(&snap); err != nil {
					fmt.Fprintln(os.Stderr, "state save failed:", err)
				}
			}
			_ = cfg
		}
	}
}

func (e *Engine) runSegment(ctx context.Context, ds *downloadState, file *os.File, idx int, cfg config.Config) error {
	ds.mu.Lock()
	if idx >= len(ds.Segments) {
		ds.mu.Unlock()
		return nil
	}
	seg := ds.Segments[idx]
	rawURL := ds.URL
	ds.mu.Unlock()

	if seg.Downloaded >= seg.End-seg.Start+1 {
		ds.mu.Lock()
		ds.Segments[idx].Done = true
		ds.mu.Unlock()
		return nil
	}

	start := seg.Start + seg.Downloaded
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, seg.End))
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if seg.Start != 0 {
			return fatalf("server ignored the Range header")
		}
	case http.StatusPartialContent:
		gotStart, _, ok := parseContentRangeStart(resp.Header.Get("Content-Range"))
		if ok && gotStart != start {
			return fmt.Errorf("server returned range starting at %d, expected %d", gotStart, start)
		}
	default:
		return &statusError{
			code: resp.StatusCode,
			msg:  fmt.Sprintf("HTTP %d for segment %d", resp.StatusCode, idx),
		}
	}

	buf := make([]byte, cfg.ChunkSize)
	var written int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if e.limiter != nil {
				if err := e.limiter.WaitN(ctx, n); err != nil {
					return err
				}
			}
			if _, err := file.WriteAt(buf[:n], start+written); err != nil {
				return err
			}
			written += int64(n)
			ds.mu.Lock()
			ds.Segments[idx].Downloaded += int64(n)
			ds.Download.Downloaded += int64(n)
			ds.mu.Unlock()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	ds.mu.Lock()
	ds.Segments[idx].Done = true
	ds.mu.Unlock()
	return nil
}

func splitSegments(total int64, maxConns int, minSize int64) []Segment {
	if total <= 0 {
		return nil
	}
	n := int((total + minSize - 1) / minSize)
	if n < 1 {
		n = 1
	}
	if n > maxConns {
		n = maxConns
	}
	if int64(n) > total {
		n = int(total)
	}
	size := total / int64(n)
	remainder := total % int64(n)
	segments := make([]Segment, 0, n)
	var start int64
	for i := 0; i < n; i++ {
		length := size
		if i < int(remainder) {
			length++
		}
		segments = append(segments, Segment{Index: i, Start: start, End: start + length - 1})
		start += length
	}
	return segments
}

func parseContentRangeStart(header string) (int64, int64, bool) {
	value := strings.TrimSpace(header)
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, false
	}
	value = strings.TrimPrefix(value, "bytes ")
	rangePart := strings.SplitN(value, "/", 2)[0]
	bounds := strings.SplitN(rangePart, "-", 2)
	if len(bounds) != 2 {
		return 0, 0, false
	}
	start, err1 := strconv.ParseInt(strings.TrimSpace(bounds[0]), 10, 64)
	end, err2 := strconv.ParseInt(strings.TrimSpace(bounds[1]), 10, 64)
	return start, end, err1 == nil && err2 == nil
}

func verifyChecksum(path string, expected *Checksum) error {
	var h hash.Hash
	switch strings.ToLower(expected.Algo) {
	case "", "sha256", "sha-256":
		h = sha256.New()
	case "md5":
		h = md5.New()
	case "sha1", "sha-1":
		h = sha1.New()
	default:
		return fatalf("unsupported checksum algorithm %q", expected.Algo)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	want := strings.ToLower(strings.TrimSpace(expected.Hex))
	if got != want {
		return fatalf("checksum mismatch: got %s, expected %s", got, want)
	}
	return nil
}
