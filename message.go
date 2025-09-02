package jsonlipc

import (
	"encoding/json"
	"fmt"
	"time"
)

// MessageType represents the type of message being sent
type MessageType string

const (
	// MessageTypeRequest represents a request message
	MessageTypeRequest MessageType = "request"
	// MessageTypeResponse represents a response message
	MessageTypeResponse MessageType = "response"
	// MessageTypeNotification represents a notification message
	MessageTypeNotification MessageType = "notification"
)

type ErrorCode string

const (
	ErrorCodeInvalidRequest ErrorCode = "invalidRequest"
	ErrorCodeMethodNotFound ErrorCode = "methodNotFound"
	ErrorCodeInvalidParams  ErrorCode = "invalidParams"
	ErrorCodeInternalError  ErrorCode = "internalError"
)

// TransportError represents an error in the TransportError communication
type TransportError struct {
	Code    ErrorCode       `json:"code"`              // Error code
	Message string          `json:"message"`           // Error message
	Details json.RawMessage `json:"details,omitempty"` // Additional error details (if any)
}

type NotificationMethod string

const (
	NotificationNotify NotificationMethod = "notify"
)

type WarningLevel string

const (
	WarnInfo WarningLevel = "info"
	WarnWarn WarningLevel = "warn"
)

type TransportWarning struct {
	Code    string       `json:"code"`
	Level   WarningLevel `json:"level"`
	Message string       `json:"message"`
	Details any          `json:"details,omitempty"` // optional structured data
}

// Message represents a JSON Lines message for IPC communication (a union of various messages for simplicity)
// Note: The Message struct is designed to be compatible with JSON Lines format because of the omitempty directives
type Message struct {
	ID         string             `json:"id,omitempty"`          // request<->response correlation; reuse on notifications
	Type       MessageType        `json:"type"`                  // request|response|notification
	Method     string             `json:"method,omitempty"`      // for requests (and optionally notifications as topic)
	Params     json.RawMessage    `json:"params,omitempty"`      // request payload (opaque to transport)
	Data       json.RawMessage    `json:"data,omitempty"`        // response/notification payload (opaque)
	Error      *TransportError    `json:"error,omitempty"`       // transport-level errors only
	Warnings   []TransportWarning `json:"warnings,omitempty"`    // transport warnings
	TS         time.Time          `json:"ts,omitempty"`          // optional timestamp
	Seq        uint64             `json:"seq,omitempty"`         // optional sequence
	DataSchema string             `json:"data_schema,omitempty"` // "message/v1"
}

// Message represents a JSON Lines message for IPC communication (a union of various messages for simplicity)
// Note: The Message struct is designed to be compatible with JSON Lines format because of the omitempty directives
// type Message struct {
// 	ID     string          `json:"id,omitempty"`     // Unique identifier for request/response correlation
// 	Type   MessageType     `json:"type"`             // Type of message
// 	Method string          `json:"method,omitempty"` // Method name for requests
// 	Params any             `json:"params,omitempty"` // Parameters for the method
// 	Data   any             `json:"data,omitempty"`   // Result data for responses
// 	Error  *TransportError `json:"error,omitempty"`  // Error information
// }

// GetTypeResult is a standalone generic function because Golang does not support generic methods
// func GetTypedResult[T any](m *Message) (T, error) {
// 	var zero T
// 	if m.Data == nil {
// 		return zero, fmt.Errorf("no result data")
// 	}

// 	// Try direct type assertion first (fastest path for simple types)
// 	if result, ok := m.Data.(T); ok {
// 		return result, nil
// 	}

// 	// Fall back to marshal/unmarshal for complex conversions
// 	data, err := json.Marshal(m.Data)
// 	if err != nil {
// 		return zero, fmt.Errorf("failed to marshal result: %w", err)
// 	}

// 	var result T
// 	if err := json.Unmarshal(data, &result); err != nil {
// 		return zero, fmt.Errorf("failed to unmarshal result: %w", err)
// 	}

// 	return result, nil
// }

// NewRequest creates a new request message
func NewRequest(id, method string, params any) (*Message, error) {

	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	p, err := marshalOrPass(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	m := Message{
		ID:     id,
		Type:   MessageTypeRequest,
		Method: method,
		Params: p,
	}
	defaultTS(&m)
	defaultSchema(&m)
	return &m, nil
}

// NewResponse builds a transport response that carries an application payload in Data.
func NewResponse(id string, data any) (*Message, error) {
	d, err := marshalOrPass(data)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}
	m := Message{
		ID:   id,
		Type: MessageTypeResponse,
		Data: d,
	}

	defaultTS(&m)
	defaultSchema(&m)
	return &m, nil
}

// NewTransportError builds a transport-level error response (malformed JSON, unknown method, etc.).
// NOTE: application-level failures should go in Data not here
func NewTransportError(id, message string, code ErrorCode, details any) *Message {
	d, err := marshalOrPass(details)
	if err != nil {
		return nil
	}
	m := Message{
		ID:   id,
		Type: MessageTypeResponse,
		Error: &TransportError{
			Code:    code,
			Message: message,
			Details: d,
		},
	}
	defaultTS(&m)
	defaultSchema(&m)
	return &m
}

// NewNotification builds a transport notification (progress, logs, partials, etc.).
func NewNotification(id string, method NotificationMethod, data any) (*Message, error) {
	d, err := marshalOrPass(data)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}
	m := Message{
		ID:     id, // reuse the original request's ID for correlation
		Type:   MessageTypeNotification,
		Method: string(method),
		Data:   d,
	}
	defaultTS(&m)
	defaultSchema(&m)
	return &m, nil
}

// ToJSON converts the message to JSON bytes
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// FromJSON creates a message from JSON bytes
func FromJSON(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}
	return &msg, nil
}

// String returns a string representation of the message
func (m *Message) String() string {
	data, _ := m.ToJSON()
	return string(data)
}

func (m *Message) IsError() bool {
	return m.Type == MessageTypeResponse && m.Error != nil
}

func defaultTS(m *Message) {
	if m.TS.IsZero() {
		m.TS = time.Now().UTC()
	}
}

func defaultSchema(m *Message) {
	if m.DataSchema == "" {
		m.DataSchema = "message/v1"
	}
}

// marshalOrPass lets callers hand in json.RawMessage to skip re-marshal
func marshalOrPass(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	if rm, ok := v.(json.RawMessage); ok {
		return rm, nil
	}
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}

func (m Message) UnmarshalData(dst any) error {
	if len(m.Data) == 0 {
		return fmt.Errorf("no data payload")
	}
	return json.Unmarshal(m.Data, dst)
}

func (m Message) UnmarshalParams(dst any) error {
	if len(m.Params) == 0 {
		return fmt.Errorf("no params payload")
	}
	return json.Unmarshal(m.Params, dst)
}

// UnmarshalDetails decodes Details into a typed struct or map.
func (e *TransportError) UnmarshalDetails(dst any) error {
	if len(e.Details) == 0 {
		return nil
	}
	return json.Unmarshal(e.Details, dst)
}
