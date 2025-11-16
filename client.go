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
	sessionID        string                     // Session ID for correlating logs/traces
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
		sessionID:     fmt.Sprintf("sess_%d", time.Now().UnixNano()),
	}
}

// Start starts the client and the underlying Python worker
func (c *Client) Start() (<-chan error, error) {
	stdErrChan, err := c.worker.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start worker: %w", err)
	}

	// Start message processing
	// Note: the message streams are created before the process runs, so this does not need to wait for successful startup
	// to run or close properly.
	go c.processMessages()

	return stdErrChan, nil
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

	// Stop the worker
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
	respChan := make(chan *Message, 10)
	c.reqMutex.Lock()
	c.pendingReqs[id] = respChan
	c.reqMutex.Unlock()

	// Send request
	request, err := NewRequest(id, method, params)
	if err != nil {
		// Clean up on error
		c.cleanupRequest(id)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// TODO: this write is blocking, we may want to handle this asynchronously
	if err := c.worker.Stream().WriteMessage(request); err != nil {
		// Clean up on error
		c.cleanupRequest(id)
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	go c.handleRequestResponse(id)
	return id, nil
}

// handleRequestResponse processes the response from Python for the given request ID
func (c *Client) handleRequestResponse(requestID string) {
	c.reqContextsMutex.RLock()
	reqCtx, exists := c.reqContexts[requestID]
	c.reqContextsMutex.RUnlock()

	if !exists {
		return
	}

	// Create and register the response channel
	respChan := make(chan *Message, 10)
	c.reqMutex.Lock()
	c.pendingReqs[requestID] = respChan
	c.reqMutex.Unlock()

	// Ensure cleanup happens
	defer c.cleanupRequest(requestID)

	ctx := c.ctx // Default to the client's context
	// Only apply timeout if it's greater than 0
	if reqCtx.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(c.ctx, reqCtx.Timeout)
		defer cancel()
	}

	for {
		select {
		case response := <-respChan:
			if response == nil { // channel has been closed
				if reqCtx.ResponseHandler != nil {
					reqCtx.ResponseHandler(nil, fmt.Errorf("request cancelled"))
				}
				return
			}

			// Process response based on type
			if response.IsError() {
				if reqCtx.ResponseHandler != nil {
					reqCtx.ResponseHandler(response, fmt.Errorf("python error: %v", response.Error))
				}
			} else {
				// Call user's response handler for processing
				if reqCtx.ResponseHandler != nil {
					reqCtx.ResponseHandler(response, nil)
				}

				if response.Type == MessageTypeResponse {
					return // Final response received; exit
				}
			}

		case <-ctx.Done():
			if reqCtx.Timeout > 0 && ctx.Err() == context.DeadlineExceeded {
				err := fmt.Errorf("request timeout after %v", reqCtx.Timeout)
				if reqCtx.ResponseHandler != nil {
					reqCtx.ResponseHandler(nil, err)
				}
			}
			// For other cancellations (like client shutdown), just return quietly
			return
		}
	}
}

// SendNotification sends a notification message to the Python process (fire-and-forget)
func (c *Client) SendNotification(method NotificationMethod, params any) error {
	if !c.worker.IsRunning() {
		return fmt.Errorf("worker is not running")
	}

	notification, err := NewNotification(c.sessionID, method, params)
	if err != nil {
		return err
	}
	return c.worker.Stream().WriteMessage(notification)
}

// OnEvent registers an event handler for a specific method
func (c *Client) OnNotification(method string, handler func(*Message)) {
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
			// LEFT-OFF: Implementing a configurable log file so the user can capture these errors
			// LEFT-OFF: may need someway for the user to notify that the process should be closed??
			// LEFT-OFF: a bunch of my tests are now failing as I am reworking what happens when a malformed message is received.
			if err != nil {
				c.worker.config.Logger.LogError(fmt.Sprintf("Error reading message from stream: %v", err))
				return
			}
			// if err != nil {
			// 	// Note: ReadChannel stops receiving messages once an error occurs; we may want for this to be more robust
			// 	fmt.Printf("Error in go-json-lipc ReadMessage() from stdio stream; closing read channel: %v\n", err)
			// 	return
			// }
			// return
		case <-c.ctx.Done():
			return
		}
	}
}

// handleMessage handles an incoming message
func (c *Client) handleMessage(msg *Message) {

	// All notifications (events, logs, etc.) are mirrored to an event handler if registered
	if msg.Type == MessageTypeNotification {
		if msg.Method != "" {
			c.handlerMutex.RLock()
			handler, exists := c.eventHandlers[msg.Method]
			c.handlerMutex.RUnlock()

			if exists {
				go handler(msg) // Run handler in goroutine to avoid blocking
			}
		}
	}

	// Request scoped messages (Responses, ProgressEvents, Warning, etc.)
	if msg.ID != "" {
		c.reqMutex.RLock()
		respChan, exists := c.pendingReqs[msg.ID]
		c.reqMutex.RUnlock()

		if exists && respChan != nil {
			respChan <- msg
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
