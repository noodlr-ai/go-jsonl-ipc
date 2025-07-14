package jsonlipc

import (
	"encoding/json"
	"fmt"
)

// MessageType represents the type of message being sent
type MessageType string

const (
	// MessageTypeRequest represents a request message
	MessageTypeRequest MessageType = "request"
	// MessageTypeResponse represents a response message
	MessageTypeResponse MessageType = "response"
	// MessageTypeError represents an error message
	MessageTypeError MessageType = "error"
	// MessageTypeEvent represents an event message
	MessageTypeEvent MessageType = "event"
)

// Message represents a JSON Lines message for IPC communication
// Note: The Message struct is designed to be compatible with JSON Lines format because of the omitempty directives
type Message struct {
	ID     string      `json:"id,omitempty"`     // Unique identifier for request/response correlation
	Type   MessageType `json:"type"`             // Type of message
	Method string      `json:"method,omitempty"` // Method name for requests
	Params any         `json:"params,omitempty"` // Parameters for the method
	Result any         `json:"result,omitempty"` // Result data for responses
	Error  *Error      `json:"error,omitempty"`  // Error information
}

// Error represents an error in the IPC communication
type Error struct {
	Code    int    `json:"code"`           // Error code
	Message string `json:"message"`        // Error message
	Data    any    `json:"data,omitempty"` // Additional error data
}

// NewRequest creates a new request message
func NewRequest(id, method string, params any) *Message {
	return &Message{
		ID:     id,
		Type:   MessageTypeRequest,
		Method: method,
		Params: params,
	}
}

// NewResponse creates a new response message
func NewResponse(id string, result any) *Message {
	return &Message{
		ID:     id,
		Type:   MessageTypeResponse,
		Result: result,
	}
}

// NewError creates a new error message
func NewError(id string, code int, message string, data any) *Message {
	return &Message{
		ID:   id,
		Type: MessageTypeError,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// NewEvent creates a new event message
func NewEvent(method string, params any) *Message {
	return &Message{
		Type:   MessageTypeEvent,
		Method: method,
		Params: params,
	}
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
