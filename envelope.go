package jsonlipc

import "time"

type Kind string

const (
	KindStarted   Kind = "started"   // lifecycle edge
	KindProgress  Kind = "progress"  // periodic update
	KindResult    Kind = "result"    // success for this scope
	KindError     Kind = "error"     // failure for this scope
	KindLog       Kind = "log"       // optional: stdout/stderr, warnings
	KindHeartbeat Kind = "heartbeat" // liveness
)

// Optional high-level state machine value you can compute in UIs/ops:
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusRetrying  Status = "retrying"
)

// Envelope wraps the "data" section of a message
type Envelope[T any] struct {
	Version   string    `json:"version"`    // "envelope/v1"
	RequestID string    `json:"request_id"` // RPC correlation
	Kind      Kind      `json:"kind"`
	TS        time.Time `json:"ts"`
	Seq       uint64    `json:"seq"` // ordered per RequestID

	// Optional state & metadata
	Status Status `json:"status,omitempty"`
	// Meta   map[string]any `json:"meta,omitempty"`

	// Progress & results
	Progress *ProgressData `json:"progress,omitempty"`
	Data     *T            `json:"data,omitempty"`  // result or partial
	Final    bool          `json:"final,omitempty"` // final for THIS scope

	// Diagnostics
	Error    *MessageError `json:"error,omitempty"`
	Warnings []MessageWarn `json:"warnings,omitempty"`
}

type ProgressData struct {
	Ratio   float64 `json:"ratio"`             // 0.0..1.0
	Stage   string  `json:"stage,omitempty"`   // "fetch", "transform", ...
	Current int64   `json:"current,omitempty"` // processed items
	Total   int64   `json:"total,omitempty"`   // estimated/known
	Unit    string  `json:"unit,omitempty"`    // "items","bytes"
	Message string  `json:"message,omitempty"`
	ETAMs   int64   `json:"eta_ms,omitempty"`
}

// MessageError represents a non-terminal error for the entire request
type MessageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	// Retryable    bool   `json:"retryable,omitempty"`
	// RetryAfterMs *int64 `json:"retry_after_ms,omitempty"`
}

type MessageWarn struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
