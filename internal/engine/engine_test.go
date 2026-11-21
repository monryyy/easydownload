package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"EasyDownload/internal/config"
	"EasyDownload/internal/store"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DownloadDir = t.TempDir()
	cfg.StateDir = filepath.Join(t.TempDir(), "state")
	cfg.RetryMaxAttempts = 3
	cfg.RetryInitialDelayMS = 20
	cfg.RetryMaxDelayMS = 100
	return cfg
}

func newTestEngine(t *testing.T, cfg *config.Config) *Engine {
	t.Helper()
	st, err := store.New(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(cfg, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// rangeServer serves a byte slice with HTTP range support.
func rangeServer(data []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"test-etag"`)
		w.Header().Set("Content-Disposition", `attachment; filename="payload.bin"`)

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			return
		}

		header := r.Header.Get("Range")
		if header == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			_, _ = w.Write(data)
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(header, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(data)) {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
}

func waitStatus(t *testing.T, e *Engine, id string, wanted ...Status) Download {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range e.List() {
			if d.ID != id {
				continue
			}
			for _, status := range wanted {
				if d.Status == status {
					return d
				}
			}
			if d.Status == StatusFailed || d.Status == StatusCanceled {
				t.Fatalf("download ended with %s: %s", d.Status, d.Error)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %v", wanted)
	return Download{}
}

func TestSplitSegments(t *testing.T) {
	segments := splitSegments(10, 3, 4)
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	var total int64
	for i, s := range segments {
		if s.Start != total {
			t.Fatalf("segment %d starts at %d, expected %d", i, s.Start, total)
		}
		total = s.End + 1
	}
	if total != 10 {
		t.Fatalf("segments cover %d bytes, expected 10", total)
	}
}

func TestSegmentedDownloadAndChecksum(t *testing.T) {
	payload := bytes.Repeat([]byte("EasyDownload test payload 0123456789"), 20000) // ~740 KB
	srv := rangeServer(payload)
	defer srv.Close()

	cfg := testConfig(t)
	cfg.MaxConnectionsPerDownload = 4
	cfg.MinSegmentSize = 64 * 1024
	e := newTestEngine(t, cfg)

	sum := sha256.Sum256(payload)
	d, err := e.Add(srv.URL, &Checksum{Algo: "sha256", Hex: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, e, d.ID, StatusCompleted)

	got, err := os.ReadFile(done.FinalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded content differs from payload")
	}
	if len(done.Segments) < 2 {
		t.Fatalf("expected segmented download, got %d segment(s)", len(done.Segments))
	}
}

func TestDownloadWithoutRangeSupport(t *testing.T) {
	payload := bytes.Repeat([]byte("abc"), 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	cfg := testConfig(t)
	cfg.MinSegmentSize = 16
	e := newTestEngine(t, cfg)
	d, err := e.Add(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, e, d.ID, StatusCompleted)
	got, err := os.ReadFile(done.FinalPath)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch: %v", err)
	}
	if len(done.Segments) != 1 {
		t.Fatalf("expected a single segment, got %d", len(done.Segments))
	}
}

func TestRetryAfterServerError(t *testing.T) {
	payload := []byte("retry test payload")
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			return
		}
		if attempts.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	cfg := testConfig(t)
	e := newTestEngine(t, cfg)
	d, err := e.Add(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, e, d.ID, StatusCompleted)
	if got, _ := os.ReadFile(done.FinalPath); !bytes.Equal(got, payload) {
		t.Fatal("content mismatch after retry")
	}
	if attempts.Load() < 2 {
		t.Fatal("expected at least one retry")
	}
}

func TestChecksumMismatch(t *testing.T) {
	payload := []byte("correct payload")
	srv := rangeServer(payload)
	defer srv.Close()

	cfg := testConfig(t)
	e := newTestEngine(t, cfg)
	d, err := e.Add(srv.URL, &Checksum{Algo: "sha256", Hex: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	d = waitStatus(t, e, d.ID, StatusFailed)
	if d.Error == "" {
		t.Fatal("expected a checksum error message")
	}
}

func TestPauseAndResume(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 300000) // 3 MB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"pause-test"`)
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", 416)
			return
		}
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		// Write slowly so pause can interrupt mid-transfer.
		chunk := 8192
		for pos := start; pos <= end; pos += int64(chunk) {
			stop := pos + int64(chunk)
			if stop > end+1 {
				stop = end + 1
			}
			_, _ = w.Write(payload[pos:stop])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}))
	defer srv.Close()

	cfg := testConfig(t)
	cfg.MaxConnectionsPerDownload = 1
	e := newTestEngine(t, cfg)
	d, err := e.Add(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current := e.List()[0]
		if current.Status == StatusActive && current.Downloaded > 100_000 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.Pause(d.ID); err != nil {
		t.Fatal(err)
	}
	paused := waitStatus(t, e, d.ID, StatusPaused)
	if paused.Downloaded <= 0 {
		t.Fatal("expected partial progress before pause")
	}

	if err := e.Resume(d.ID); err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, e, d.ID, StatusCompleted)
	got, err := os.ReadFile(done.FinalPath)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("resumed content mismatch: %v", err)
	}
}

type eventRecorder struct {
	mu       sync.Mutex
	counts   map[string]int
	progress []ProgressEvent
}

func (r *eventRecorder) record(name string, data interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = make(map[string]int)
	}
	r.counts[name]++
	if name == EventProgress {
		if payload, ok := data.(ProgressEvent); ok {
			r.progress = append(r.progress, payload)
		}
	}
}

func (r *eventRecorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[name]
}

func (r *eventRecorder) lastProgress() ProgressEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.progress) == 0 {
		return ProgressEvent{}
	}
	return r.progress[len(r.progress)-1]
}

// The frontend listens for "download:progress"; an emitted "progress" event
// would be silently dropped and leave the UI frozen until restart.
func TestProgressEventName(t *testing.T) {
	payload := bytes.Repeat([]byte("progress-event"), 20000)
	srv := rangeServer(payload)
	defer srv.Close()

	cfg := testConfig(t)
	st, err := store.New(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	rec := &eventRecorder{}
	e, err := New(cfg, st, rec.record)
	if err != nil {
		t.Fatal(err)
	}

	d, err := e.Add(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, e, d.ID, StatusCompleted)

	if rec.count("progress") != 0 {
		t.Fatal("emitted an unrecognised 'progress' event name")
	}
	if rec.count(EventProgress) == 0 {
		t.Fatalf("no %q events emitted; got: %v", EventProgress, rec.counts)
	}
	last := rec.lastProgress()
	if last.ID != d.ID {
		t.Fatalf("progress event has ID %q, want %q", last.ID, d.ID)
	}
	if last.Total != int64(len(payload)) {
		t.Fatalf("unexpected progress payload: %+v", last)
	}
}
