package jsonlipc

import "encoding/json"

type Kind string

const (
	KindStarted  Kind = "started"  // lifecycle edge
	KindProgress Kind = "progress" // periodic update
	KindResult   Kind = "result"   // success for this scope
	KindError    Kind = "error"    // failure for this scope
	KindLog      Kind = "log"      // optional: stdout/stderr, warnings
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

// EnvelopeKind represents the type of envelope
type EnvelopeKind string

const (
	EnvelopeKindProgress EnvelopeKind = "progress"
	EnvelopeKindResult   EnvelopeKind = "result"
	EnvelopeKindError    EnvelopeKind = "error"
	EnvelopeKindLog      EnvelopeKind = "log"
)

// ErrorMessage represents an error with code, message and optional details
type ErrorMessage struct {
	Code    ErrorCode       `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"` // parse to map[string]any if needed
}

// ProgressData represents progress information for an operation
type ProgressData struct {
	Ratio   float64 `json:"ratio"`             // 0.0..1.0
	Current float64 `json:"current"`           // processed items
	Total   float64 `json:"total"`             // estimated/known
	Unit    string  `json:"unit"`              // "items","bytes"
	Stage   string  `json:"stage,omitempty"`   // "fetch", "transform", etc.
	Message string  `json:"message,omitempty"` // optional message
	ETAMs   int     `json:"eta_ms,omitempty"`  // estimated time to completion in ms
}

type Envelope interface {
	GetKind() EnvelopeKind
}

// ResultEnvelope represents a result envelope
type ResultEnvelope struct {
	Kind      EnvelopeKind    `json:"kind"`   // "result"
	Schema    string          `json:"schema"` // "envelope/v1"
	RequestID string          `json:"request_id"`
	TS        string          `json:"ts"`
	Data      json.RawMessage `json:"data"` // map[string]any
	Final     bool            `json:"final"`
	Messages  []LogMessage    `json:"messages"`
	Seq       int             `json:"seq,omitempty"` // Will be injected by the worker
}

func (e ResultEnvelope) GetKind() EnvelopeKind { return e.Kind }
func (e *ResultEnvelope) UnmarshalDataPayload(dst any) error {
	return json.Unmarshal(e.Data, dst)
}

// ErrorEnvelope represents an error envelope
type ErrorEnvelope struct {
	Kind      EnvelopeKind    `json:"kind"`   // "error"
	Schema    string          `json:"schema"` // "envelope/v1"
	RequestID string          `json:"request_id"`
	TS        string          `json:"ts"`
	Error     ErrorMessage    `json:"error"`
	Final     bool            `json:"final"`
	Status    Status          `json:"status"`
	Messages  []LogMessage    `json:"messages"`
	Details   json.RawMessage `json:"details,omitempty"` // parse to map[string]any if needed
	Seq       int             `json:"seq,omitempty"`     // Will be injected by the worker
}

func (e ErrorEnvelope) GetKind() EnvelopeKind { return e.Kind }

// LogEnvelope represents a log envelope
type LogEnvelope struct {
	Kind      EnvelopeKind `json:"kind"`   // "log"
	Schema    string       `json:"schema"` // "envelope/v1"
	TS        string       `json:"ts"`
	Messages  []LogMessage `json:"messages"`
	RequestID string       `json:"request_id,omitempty"`
	Seq       int          `json:"seq,omitempty"` // Will be injected by the worker
}

func (e LogEnvelope) GetKind() EnvelopeKind { return e.Kind }

// ProgressEnvelope represents a progress envelope
type ProgressEnvelope struct {
	Kind      EnvelopeKind `json:"kind"`   // "progress"
	Schema    string       `json:"schema"` // "envelope/v1"
	RequestID string       `json:"request_id"`
	TS        string       `json:"ts"`
	Progress  ProgressData `json:"progress"`
	Status    Status       `json:"status"`
	Messages  []LogMessage `json:"messages"`
	Seq       int          `json:"seq,omitempty"` // Will be injected by the worker
}

func (e ProgressEnvelope) GetKind() EnvelopeKind { return e.Kind }

// Envelope wraps the "data" section of a message
// type Envelope[T any] struct {
// 	Version   string    `json:"version"`    // "envelope/v1"
// 	RequestID string    `json:"request_id"` // RPC correlation
// 	Kind      Kind      `json:"kind"`
// 	TS        time.Time `json:"ts"`
// 	Seq       uint64    `json:"seq"` // ordered per RequestID

// 	// Optional state & metadata
// 	Status Status `json:"status,omitempty"`
// 	// Meta   map[string]any `json:"meta,omitempty"`

// 	// Progress & results
// 	Progress *ProgressData `json:"progress,omitempty"`
// 	Data     *T            `json:"data,omitempty"`  // result or partial
// 	Final    bool          `json:"final,omitempty"` // final for THIS scope

// 	// Diagnostics
// 	Error    *MessageError `json:"error,omitempty"`
// 	Warnings []MessageWarn `json:"warnings,omitempty"`
// }
