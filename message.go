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

type LogLevel string

const (
	WarnInfo  LogLevel = "info"
	WarnError LogLevel = "error"
	WarnWarn  LogLevel = "warn"
	WarnDebug LogLevel = "debug"
)

type LogMessage struct {
	Level   LogLevel        `json:"level"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"` // Can be map[string]any or ErrorMessage
}

// Message represents a JSON Lines message for IPC communication (a union of various messages for simplicity)
// Note: The Message struct is designed to be compatible with JSON Lines format because of the omitempty directives
type Message struct {
	ID       string          `json:"id,omitempty"`       // request<->response correlation; reuse on notifications
	Type     MessageType     `json:"type"`               // request|response|notification
	TS       time.Time       `json:"ts"`                 // timestamp
	Method   string          `json:"method,omitempty"`   // for requests (and optionally notifications as topic)
	Params   json.RawMessage `json:"params,omitempty"`   // request payload (opaque to transport)
	Data     json.RawMessage `json:"data,omitempty"`     // response/notification payload (opaque)
	Error    *TransportError `json:"error,omitempty"`    // transport-level errors only
	Messages []LogMessage    `json:"messages,omitempty"` // transport messages
	Seq      uint64          `json:"seq,omitempty"`      // optional sequence
	Schema   string          `json:"schema,omitempty"`   // "message/v1"
}

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
	if m.Schema == "" {
		m.Schema = "message/v1"
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

// UnmarshalData will unmarshal the "data" property of the message struct
func (m Message) UnmarshalData(dst any) error {
	if len(m.Data) == 0 {
		return fmt.Errorf("no data payload")
	}
	return json.Unmarshal(m.Data, dst)
}

// UnmarshalDataPayload will unmarshal a nested data object assuming a structure of message["data"]["data"] where the nested "data" property is the payload
func (m Message) UnmarshalDataPayload(dst any) error {
	if len(m.Data) == 0 {
		return fmt.Errorf("no data payload")
	}

	// First unmarshal into a struct with a "data" field
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(m.Data, &envelope); err != nil {
		return fmt.Errorf("failed to unmarshal envelope: %w", err)
	}

	if len(envelope.Data) == 0 {
		return fmt.Errorf("no nested data payload")
	}

	// Then unmarshal the nested data into the destination
	return json.Unmarshal(envelope.Data, dst)
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

// GetEnvelopeKind returns the kind of envelope in the Data field without fully unmarshaling
func (m Message) GetEnvelopeKind() (EnvelopeKind, error) {
	if len(m.Data) == 0 {
		return "", fmt.Errorf("no data payload")
	}

	var envelope struct {
		Kind EnvelopeKind `json:"kind"`
	}
	if err := json.Unmarshal(m.Data, &envelope); err != nil {
		return "", fmt.Errorf("failed to unmarshal envelope kind: %w", err)
	}

	return envelope.Kind, nil
}

// UnmarshalEnvelope unmarshals Message.Data into the appropriate envelope type based on the "kind" field
func (m Message) UnmarshalEnvelope() (Envelope, error) {
	if len(m.Data) == 0 {
		return nil, fmt.Errorf("no data payload")
	}

	// First, peek at the kind field to determine the envelope type
	kind, err := m.GetEnvelopeKind()
	if err != nil {
		return nil, err
	}

	// Based on the kind, unmarshal into the specific envelope type
	switch kind {
	case EnvelopeKindResult:
		var result ResultEnvelope
		if err := json.Unmarshal(m.Data, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ResultEnvelope: %w", err)
		}
		return &result, nil

	case EnvelopeKindError:
		var errorEnv ErrorEnvelope
		if err := json.Unmarshal(m.Data, &errorEnv); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ErrorEnvelope: %w", err)
		}
		return &errorEnv, nil

	case EnvelopeKindLog:
		var logEnv LogEnvelope
		if err := json.Unmarshal(m.Data, &logEnv); err != nil {
			return nil, fmt.Errorf("failed to unmarshal LogEnvelope: %w", err)
		}
		return &logEnv, nil

	case EnvelopeKindProgress:
		var progressEnv ProgressEnvelope
		if err := json.Unmarshal(m.Data, &progressEnv); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ProgressEnvelope: %w", err)
		}
		return &progressEnv, nil
	default:
		return nil, fmt.Errorf("unknown envelope kind: %s", kind)
	}
}
