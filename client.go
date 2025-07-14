package jsonlipc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type RequestContext struct {
	Id              string
	ResponseHandler func(*Message, error) // Called when Python responds to this request
	CreatedAt       time.Time
	Timeout         time.Duration
	cancelled       bool
	mutex           sync.Mutex // Mutex to protect access to cancelled state
}

// Client provides a high-level interface for communicating with Python processes
type Client struct {
	worker           *Worker
	pendingReqs      map[string]chan *Message
	reqMutex         sync.RWMutex // Mutex for pending requests
	requestCounter   atomic.Int64
	eventHandlers    map[string]func(*Message)
	handlerMutex     sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	reqContexts      map[string]*RequestContext // Track request contexts for cancellation
	reqContextsMutex sync.RWMutex               // Mutex for request context management
}

// NewClient creates a new client with the given worker configuration
func NewClient(config WorkerConfig) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		worker:        NewWorker(config),
		pendingReqs:   make(map[string]chan *Message),
		eventHandlers: make(map[string]func(*Message)),
		reqContexts:   make(map[string]*RequestContext),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start starts the client and the underlying Python worker
func (c *Client) Start() error {
	if err := c.worker.Start(); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}

	// Start message processing
	go c.processMessages()

	return nil
}

// Stop stops the client and the underlying Python worker
func (c *Client) Stop() error {
	c.cancel()

	// Close all pending request channels
	c.reqMutex.Lock()
	for _, ch := range c.pendingReqs {
		close(ch)
	}
	c.pendingReqs = make(map[string]chan *Message)
	c.reqMutex.Unlock()

	// Clear request contexts
	c.reqContextsMutex.Lock()
	c.reqContexts = make(map[string]*RequestContext)
	c.reqContextsMutex.Unlock()

	return c.worker.Stop()
}

// SendRequest sends an RPC request to the Python process and waits for a response in a separate goroutine
func (c *Client) SendRequest(method string, params any) (string, error) {
	return c.SendRequestWithHandler(method, params, nil)
}

// SendRequestWithHandler sends an RPC request with a custom response handler
func (c *Client) SendRequestWithHandler(method string, params any, handler func(*Message, error)) (string, error) {
	return c.sendRequestWithTimeoutAndHandler(method, params, 0, handler)
}

// SendRequestWithTimeout sends an RPC request with a timeout
func (c *Client) SendRequestWithTimeout(method string, params any, timeout int) (string, error) {
	return c.sendRequestWithTimeoutAndHandler(method, params, timeout, nil)
}

// SendRequestWithTimeoutAndHandler sends an RPC request with both timeout and response handler
func (c *Client) SendRequestWithTimeoutAndHandler(method string, params any, timeout int, handler func(*Message, error)) (string, error) {
	return c.sendRequestWithTimeoutAndHandler(method, params, timeout, handler)
}

// sendRequestWithTimeoutAndHandler makes a synchronous RPC call with a timeout and response handler
func (c *Client) sendRequestWithTimeoutAndHandler(method string, params any, timeout int, handler func(*Message, error)) (string, error) {
	if !c.worker.IsRunning() {
		return "", fmt.Errorf("worker is not running")
	}

	// Generate unique request ID
	id := fmt.Sprintf("req_%d", c.requestCounter.Add(1))

	// Create request context (allows for cancellation and cleaning up)
	reqCtx := &RequestContext{
		Id:              id,
		ResponseHandler: handler,
		CreatedAt:       time.Now(),
		Timeout:         time.Duration(timeout) * time.Second,
	}

	// Store request context for cancellation
	c.reqContextsMutex.Lock()
	c.reqContexts[id] = reqCtx
	c.reqContextsMutex.Unlock()

	// Create response channel
	respChan := make(chan *Message, 1)
	c.reqMutex.Lock()
	c.pendingReqs[id] = respChan
	c.reqMutex.Unlock()

	// Send request
	request := NewRequest(id, method, params)

	// TODO: this write is blocking, we may want to handle this asynchronously
	if err := c.worker.Stream().WriteMessage(request); err != nil {
		// Clean up on error
		c.cleanupRequest(id)
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	go c.handleRequestResponse(id, respChan)
	return id, nil
}

// handleRequestResponse processes the response from Python for the given request ID
func (c *Client) handleRequestResponse(requestID string, respChan <-chan *Message) {
	defer c.cleanupRequest(requestID)

	c.reqContextsMutex.RLock()
	reqCtx, exists := c.reqContexts[requestID]
	c.reqContextsMutex.RUnlock()

	if !exists {
		return
	}

	ctx := c.ctx // Default to the client's context

	// Only apply timeout if it's greater than 0
	if reqCtx.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(c.ctx, reqCtx.Timeout)
		defer cancel()
	}

	select {
	case response := <-respChan:
		if response == nil { // channel has been closed
			if reqCtx.ResponseHandler != nil {
				reqCtx.ResponseHandler(nil, fmt.Errorf("request cancelled"))
			}
			return
		}

		// Process response based on type
		if response.Type == MessageTypeError {
			if reqCtx.ResponseHandler != nil {
				reqCtx.ResponseHandler(response, fmt.Errorf("python error: %v", response.Error))
			}
		} else {
			// Call user's response handler for processing
			if reqCtx.ResponseHandler != nil {
				reqCtx.ResponseHandler(response, nil)
			}
		}

	case <-ctx.Done():
		// Only handle if timeout > 0
		if reqCtx.Timeout <= 0 {
			return // No timeout, just return
		}

		var err error
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("request timeout after %v", reqCtx.Timeout)
		} else {
			err = fmt.Errorf("request cancelled: %w", ctx.Err())
		}

		if reqCtx.ResponseHandler != nil {
			reqCtx.ResponseHandler(nil, err)
		}
	}
}

// SendEvent sends an event message to the Python process (fire-and-forget)
func (c *Client) SendEvent(method string, params any) error {
	if !c.worker.IsRunning() {
		return fmt.Errorf("worker is not running")
	}

	event := NewEvent(method, params)
	return c.worker.Stream().WriteMessage(event)
}

// OnEvent registers an event handler for a specific method
// TODO: we may want this to run in a goroutine to avoid blocking the main thread
func (c *Client) OnEvent(method string, handler func(*Message)) {
	c.handlerMutex.Lock()
	defer c.handlerMutex.Unlock()
	c.eventHandlers[method] = handler
}

// RemoveEventHandler removes an event handler for a specific method
func (c *Client) RemoveEventHandler(method string) {
	c.handlerMutex.Lock()
	defer c.handlerMutex.Unlock()
	delete(c.eventHandlers, method)
}

// processMessages processes incoming messages from the Python worker
func (c *Client) processMessages() {
	if c.worker.Stream() == nil {
		return
	}

	msgChan, errChan := c.worker.Stream().ReadChannel()

	for {
		select {
		case msg := <-msgChan:
			if msg == nil {
				return // Channel closed
			}
			c.handleMessage(msg)
		case err := <-errChan:
			if err != nil {
				// Handle error - for now just continue
				// In a more advanced implementation, you might want to
				// provide error callbacks or restart the worker
				continue
			}
			return
		case <-c.ctx.Done():
			return
		}
	}
}

// handleMessage handles an incoming message
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case MessageTypeResponse, MessageTypeError:
		// Handle response/error for pending request
		if msg.ID != "" {
			c.reqMutex.RLock()
			respChan, exists := c.pendingReqs[msg.ID]
			c.reqMutex.RUnlock()

			// TODO: Handle case where response channel is closed or full
			// If the channel is full, we skip sending the message to avoid blocking (this is not ideal...)
			if exists {
				select {
				case respChan <- msg:
				default:
					// Channel might be full or closed
				}
			}
		}
	case MessageTypeEvent:
		// Handle event
		if msg.Method != "" {
			c.handlerMutex.RLock()
			handler, exists := c.eventHandlers[msg.Method]
			c.handlerMutex.RUnlock()

			if exists {
				go handler(msg) // Run handler in goroutine to avoid blocking
			}
		}
	}
}

// IsRunning returns whether the client and worker are running
func (c *Client) IsRunning() bool {
	return c.worker.IsRunning()
}

// CancelRequest cancels a pending request
func (c *Client) CancelRequest(requestID string) {
	c.reqContextsMutex.Lock()
	if reqCtx, exists := c.reqContexts[requestID]; exists {
		reqCtx.mutex.Lock()
		reqCtx.cancelled = true
		reqCtx.mutex.Unlock()
	}
	c.reqContextsMutex.Unlock()

	c.cleanupRequest(requestID)
}

// cleanupRequest removes request from all tracking maps
func (c *Client) cleanupRequest(requestID string) {
	c.reqMutex.Lock()
	if respChan, exists := c.pendingReqs[requestID]; exists {
		close(respChan)
		delete(c.pendingReqs, requestID)
	}
	c.reqMutex.Unlock()

	c.reqContextsMutex.Lock()
	delete(c.reqContexts, requestID)
	c.reqContextsMutex.Unlock()
}
