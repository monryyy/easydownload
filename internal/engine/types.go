package engine

import "EasyDownload/internal/model"

// Event names shared with the frontend (frontend/src/main.js).
const (
	EventState    = "download:state"
	EventProgress = "download:progress"
	EventComplete = "download:complete"
	EventError    = "download:error"
)

// Aliases keep the engine API stable while the shared definitions live in model.
type Status = model.Status
type Checksum = model.Checksum
type Segment = model.Segment
type Download = model.Download
type ProgressEvent = model.ProgressEvent
type StateEvent = model.StateEvent
type CompleteEvent = model.CompleteEvent
type ErrorEvent = model.ErrorEvent

const (
	StatusQueued    = model.StatusQueued
	StatusActive    = model.StatusActive
	StatusPaused    = model.StatusPaused
	StatusCompleted = model.StatusCompleted
	StatusFailed    = model.StatusFailed
	StatusCanceled  = model.StatusCanceled
)
