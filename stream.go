package jsonlipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Stream handles reading and writing JSON Lines messages
type Stream struct {
	scanner *bufio.Scanner
	writer  io.Writer
	mutex   sync.Mutex
}

// NewStream creates a new stream for JSON Lines communication
func NewStream(reader io.Reader, writer io.Writer) *Stream {
	return &Stream{
		scanner: bufio.NewScanner(reader),
		writer:  writer,
	}
}

// ReadMessage reads a single JSON Lines message from the stream
func (s *Stream) ReadMessage() (*Message, error) {
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to scan line: %w", err)
		}
		// EOF reached
		return nil, io.EOF
	}

	line := s.scanner.Bytes()

	// Skip empty lines
	if len(line) == 0 {
		return s.ReadMessage()
	}

	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

// WriteMessage writes a JSON Lines message to the stream
func (s *Stream) WriteMessage(msg *Message) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Write the JSON followed by a newline
	if _, err := s.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if _, err := s.writer.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Note: stdout does not support flushing; uncomment this if needed for a different writer
	// Flush if the writer supports it
	// if flusher, ok := s.writer.(interface{ Flush() error }); ok {
	// 	if err := flusher.Flush(); err != nil {
	// 		return fmt.Errorf("failed to flush: %w", err)
	// 	}
	// }

	return nil
}

// ReadChannel returns a channel that receives messages from the stream
func (s *Stream) ReadChannel() (<-chan *Message, <-chan error) {
	msgChan := make(chan *Message)
	errChan := make(chan error, 1)

	go func() {
		defer close(msgChan)
		defer close(errChan)

		for {
			msg, err := s.ReadMessage()
			if err != nil {
				if err != io.EOF {
					errChan <- err
				}
				return // Note: we may need to handle EOF differently if the Python process unexpectedly terminates
			}
			msgChan <- msg
		}
	}()

	return msgChan, errChan
}
