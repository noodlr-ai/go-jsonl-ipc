package jsonlipc

import (
	"testing"
	"time"
)

func TestMessage(t *testing.T) {
	// Test request message
	req := NewRequest("test1", "ping", nil)
	if req.ID != "test1" {
		t.Errorf("Expected ID 'test1', got '%s'", req.ID)
	}
	if req.Type != MessageTypeRequest {
		t.Errorf("Expected type '%s', got '%s'", MessageTypeRequest, req.Type)
	}
	if req.Method != "ping" {
		t.Errorf("Expected method 'ping', got '%s'", req.Method)
	}

	// Test response message
	resp := NewResponse("test1", "pong")
	if resp.ID != "test1" {
		t.Errorf("Expected ID 'test1', got '%s'", resp.ID)
	}
	if resp.Type != MessageTypeResponse {
		t.Errorf("Expected type '%s', got '%s'", MessageTypeResponse, resp.Type)
	}
	if resp.Data != "pong" {
		t.Errorf("Expected result 'pong', got '%v'", resp.Data)
	}

	// Test error message
	errMsg := NewIPCError("test1", -1, "test error", nil)
	if errMsg.ID != "test1" {
		t.Errorf("Expected ID 'test1', got '%s'", errMsg.ID)
	}
	if errMsg.Type != MessageTypeError {
		t.Errorf("Expected type '%s', got '%s'", MessageTypeError, errMsg.Type)
	}
	if errMsg.Error.Code != -1 {
		t.Errorf("Expected error code -1, got %d", errMsg.Error.Code)
	}
	if errMsg.Error.Message != "test error" {
		t.Errorf("Expected error message 'test error', got '%s'", errMsg.Error.Message)
	}

	// Test event message
	event := NewEvent("notify", map[string]string{"msg": "hello"})
	if event.Type != MessageTypeEvent {
		t.Errorf("Expected type '%s', got '%s'", MessageTypeEvent, event.Type)
	}
	if event.Method != "notify" {
		t.Errorf("Expected method 'notify', got '%s'", event.Method)
	}
}

func TestMessageJSON(t *testing.T) {
	// Test JSON marshaling
	req := NewRequest("test1", "ping", map[string]int{"num": 42})

	data, err := req.ToJSON()
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	// Test JSON unmarshaling
	msg, err := FromJSON(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if msg.ID != req.ID {
		t.Errorf("Expected ID '%s', got '%s'", req.ID, msg.ID)
	}
	if msg.Type != req.Type {
		t.Errorf("Expected type '%s', got '%s'", req.Type, msg.Type)
	}
	if msg.Method != req.Method {
		t.Errorf("Expected method '%s', got '%s'", req.Method, msg.Method)
	}
}

func TestWorkerConfig(t *testing.T) {
	config := WorkerConfig{
		ScriptPath: "test.py",
		Timeout:    10 * time.Second,
	}

	worker := NewWorker(config)
	if worker == nil {
		t.Fatal("NewWorker returned nil")
	}

	if worker.config.PythonPath != "python3" {
		t.Errorf("Expected default python path 'python3', got '%s'", worker.config.PythonPath)
	}

	if worker.config.ScriptPath != "test.py" {
		t.Errorf("Expected script path 'test.py', got '%s'", worker.config.ScriptPath)
	}

	if worker.config.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", worker.config.Timeout)
	}
}

func TestClientCreation(t *testing.T) {
	config := WorkerConfig{
		ScriptPath: "test.py",
	}

	client := NewClient(config)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.IsRunning() {
		t.Error("Client should not be running immediately after creation")
	}
}

// Test RequestContext creation and management
func TestRequestContext(t *testing.T) {
	responseReceived := false
	var receivedMessage *Message
	var receivedError error

	// Create a request context with response handler
	reqCtx := &RequestContext{
		Id:        "test_req_1",
		CreatedAt: time.Now(),
		Timeout:   5 * time.Second,
		ResponseHandler: func(msg *Message, err error) {
			responseReceived = true
			receivedMessage = msg
			receivedError = err
		},
	}

	if reqCtx.Id != "test_req_1" {
		t.Errorf("Expected ID 'test_req_1', got '%s'", reqCtx.Id)
	}

	if reqCtx.Timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", reqCtx.Timeout)
	}

	// Test response handler
	testMsg := NewResponse("test_req_1", "test result")
	reqCtx.ResponseHandler(testMsg, nil)

	if !responseReceived {
		t.Error("Response handler was not called")
	}

	if receivedMessage != testMsg {
		t.Error("Response handler received wrong message")
	}

	if receivedError != nil {
		t.Errorf("Response handler received unexpected error: %v", receivedError)
	}
}

// Test client event handling
func TestClientEventHandling(t *testing.T) {
	config := WorkerConfig{
		ScriptPath: "test.py",
	}

	client := NewClient(config)
	defer client.Stop()

	eventReceived := false
	var receivedEvent *Message

	// Register event handler
	client.OnEvent("test_event", func(msg *Message) {
		eventReceived = true
		receivedEvent = msg
	})

	// Simulate receiving an event message
	testEvent := NewEvent("test_event", map[string]string{"data": "test"})
	client.handleMessage(testEvent)

	// Give some time for the handler to be called
	time.Sleep(10 * time.Millisecond)

	if !eventReceived {
		t.Error("Event handler was not called")
	}

	if receivedEvent.Method != "test_event" {
		t.Errorf("Expected event method 'test_event', got '%s'", receivedEvent.Method)
	}

	// Test removing event handler
	client.RemoveEventHandler("test_event")

	// Reset for next test
	eventReceived = false
	receivedEvent = nil

	// Send another event - should not be handled
	client.handleMessage(testEvent)
	time.Sleep(10 * time.Millisecond)

	if eventReceived {
		t.Error("Event handler should have been removed")
	}
}

// Test async request functionality
func TestClientAsyncRequest(t *testing.T) {
	config := WorkerConfig{
		ScriptPath: "test.py",
	}

	client := NewClient(config)
	defer client.Stop()

	// Test that SendRequest returns a request ID without starting the worker
	requestID, err := client.SendRequest("test_method", map[string]string{"param": "value"})

	// Should fail because worker is not running
	if err == nil {
		t.Error("Expected error when worker is not running, got nil")
	}

	if requestID != "" {
		t.Error("Expected empty request ID when worker is not running")
	}
}

// Test request cancellation
func TestRequestCancellation(t *testing.T) {
	config := WorkerConfig{
		ScriptPath: "test.py",
	}

	client := NewClient(config)
	defer client.Stop()

	// Create a request context manually for testing
	reqCtx := &RequestContext{
		Id:        "cancel_test",
		CreatedAt: time.Now(),
		Timeout:   5 * time.Second,
	}

	// Add to client's request contexts
	client.reqContextsMutex.Lock()
	if client.reqContexts == nil {
		client.reqContexts = make(map[string]*RequestContext)
	}
	client.reqContexts["cancel_test"] = reqCtx
	client.reqContextsMutex.Unlock()

	// Test cancellation
	client.CancelRequest("cancel_test")

	// Verify request was removed
	client.reqContextsMutex.RLock()
	_, exists := client.reqContexts["cancel_test"]
	client.reqContextsMutex.RUnlock()

	if exists {
		t.Error("Request context should have been removed after cancellation")
	}
}

// Test message handling for responses
func TestMessageResponseHandling(t *testing.T) {
	config := WorkerConfig{
		ScriptPath: "test.py",
	}

	client := NewClient(config)
	defer client.Stop()

	// Create a pending request channel
	respChan := make(chan *Message, 1)
	client.reqMutex.Lock()
	client.pendingReqs["test_response"] = respChan
	client.reqMutex.Unlock()

	// Create a response message
	response := NewResponse("test_response", "test result")

	// Handle the message
	client.handleMessage(response)

	// Check that the response was sent to the channel
	select {
	case receivedMsg := <-respChan:
		if receivedMsg.ID != "test_response" {
			t.Errorf("Expected response ID 'test_response', got '%s'", receivedMsg.ID)
		}
		if receivedMsg.Data != "test result" {
			t.Errorf("Expected result 'test result', got '%v'", receivedMsg.Data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Response was not received on channel")
	}
}
