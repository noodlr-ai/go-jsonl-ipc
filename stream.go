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
	sc := bufio.NewScanner(reader)
	sc.Buffer(make([]byte, 0, 64*1024), 5*1024*1024) // allow big JSON lines (5MB)
	return &Stream{
		scanner: sc,
		writer:  writer,
	}
}

// ReadMessage reads a single JSON Lines message from the stream
func (s *Stream) ReadMessage() (*Message, error) {
	// Note: we use a for-loop and continue instead of calling ReadMessage() recursively when we encounter an empty line
	for {
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
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, fmt.Errorf("(warning) improperly formed message: %s", string(line))
		}

		return &msg, nil
	}
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
	msgChan := make(chan *Message, 10)
	errChan := make(chan error, 10)

	go func() {
		defer close(msgChan)
		defer close(errChan)

		for {
			msg, err := s.ReadMessage()
			if err == nil {
				msgChan <- msg
				continue
			}
			if err == io.EOF {
				return // child closed stdout; we'er done
			}
			// send error if possible; otherwise drop to avoid blocking
			select {
			case errChan <- err:
				// Note: it is possible for ReadMessage() to return a "file already closed" when the engine is shutdown
			default:
			}
			// TODO: may want this to be a continue instead
			return // stop draining on persistent error
		}
	}()

	return msgChan, errChan
}
