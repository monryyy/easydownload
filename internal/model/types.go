// Package model contains types shared by the engine and the state store.
package model

import "time"

// Status describes where a download is in its lifecycle.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Checksum is an optional expected hash for a completed file.
type Checksum struct {
	Algo string `json:"algo"`
	Hex  string `json:"hex"`
}

// Segment is one byte range of a download.
type Segment struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"` // inclusive
	Downloaded int64 `json:"downloaded"`
	Done       bool  `json:"done"`
}

// Download is the serialisable state of a single download.
type Download struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	Name       string    `json:"name"`
	FinalPath  string    `json:"finalPath"`
	PartPath   string    `json:"partPath"`
	TotalSize  int64     `json:"totalSize"`
	Downloaded int64     `json:"downloaded"`
	ETag       string    `json:"etag"`
	LastMod    string    `json:"lastModified"`
	RangeOK    bool      `json:"rangeOk"`
	Probed     bool      `json:"probed"`
	Status     Status    `json:"status"`
	Segments   []Segment `json:"segments"`
	Checksum   *Checksum `json:"checksum,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Clone returns a deep copy so callers can safely read it while workers write.
func (d *Download) Clone() Download {
	c := *d
	if d.Segments != nil {
		c.Segments = append([]Segment(nil), d.Segments...)
	}
	if d.Checksum != nil {
		ck := *d.Checksum
		c.Checksum = &ck
	}
	return c
}

// ProgressEvent is emitted every 500ms while a download is active.
type ProgressEvent struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Speed      float64 `json:"speed"` // bytes per second
	ETA        int64   `json:"eta"`   // seconds, -1 when unknown
}

// StateEvent is emitted whenever a download's status changes.
type StateEvent struct {
	Download Download `json:"download"`
}

// CompleteEvent is emitted after verification and rename.
type CompleteEvent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// ErrorEvent is emitted when a download fails permanently.
type ErrorEvent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}
