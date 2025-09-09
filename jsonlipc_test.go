package jsonlipc

import (
	"testing"
	"time"
)

func TestMessage(t *testing.T) {
	// Test request message
	req, err := NewRequest("test1", "ping", nil)

	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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
	resp, err := NewResponse("test1", "pong")
	if err != nil {
		t.Fatalf("Failed to create response: %v", err)
	}
	if resp.ID != "test1" {
		t.Errorf("Expected ID 'test1', got '%s'", resp.ID)
	}
	if resp.Type != MessageTypeResponse {
		t.Errorf("Expected type '%s', got '%s'", MessageTypeResponse, resp.Type)
	}

	var result string
	err = resp.UnmarshalData(&result)
	if err != nil {
		t.Errorf("Failed to unmarshal data: %v", err)
	}
	if result != "pong" {
		t.Errorf("Expected result 'pong', got '%v'", result)
	}

	// Test error message
	errMsg := NewTransportError("test1", "test error", ErrorCodeInternalError, nil)

	if errMsg.ID != "test1" {
		t.Errorf("Expected ID 'test1', got '%s'", errMsg.ID)
	}
	if !errMsg.IsError() {
		t.Errorf("Expected IsError() to be true")
	}
	if errMsg.Error.Code != ErrorCodeInternalError {
		t.Errorf("Expected error code %s, got %s", ErrorCodeInternalError, errMsg.Error.Code)
	}
	if errMsg.Error.Message != "test error" {
		t.Errorf("Expected error message 'test error', got '%s'", errMsg.Error.Message)
	}

	// Test event message
	event, err := NewNotification("sess_1234", NotificationNotify, map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("Failed to create notification: %v", err)
	}
	if event.Type != MessageTypeNotification {
		t.Errorf("Expected type '%s', got '%s'", MessageTypeNotification, event.Type)
	}
	if event.Method != "notify" {
		t.Errorf("Expected method 'notify', got '%s'", event.Method)
	}
}

func TestMessageJSON(t *testing.T) {
	// Test JSON marshaling
	req, err := NewRequest("test1", "ping", map[string]int{"num": 42})
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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
	testMsg, err := NewResponse("test_req_1", "test result")
	if err != nil {
		t.Fatalf("Failed to create response: %v", err)
	}
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
	client.OnNotification("test_event", func(msg *Message) {
		eventReceived = true
		receivedEvent = msg
	})

	// Simulate receiving an event message
	testEvent, err := NewNotification("sess_1234", "test_event", map[string]string{"data": "test"})
	if err != nil {
		t.Fatalf("Failed to create notification: %v", err)
	}
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
	response, err := NewResponse("test_response", "test result")
	if err != nil {
		t.Fatalf("Failed to create response: %v", err)
	}

	// Handle the message
	client.handleMessage(response)

	// Check that the response was sent to the channel
	select {
	case receivedMsg := <-respChan:
		if receivedMsg.ID != "test_response" {
			t.Errorf("Expected response ID 'test_response', got '%s'", receivedMsg.ID)
		}
		var data string
		if err := receivedMsg.UnmarshalData(&data); err != nil {
			t.Errorf("Failed to unmarshal response data: %v", err)
		}
		if data != "test result" {
			t.Errorf("Expected result 'test result', got '%v'", data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Response was not received on channel")
	}
}

// Test transport warning functionality
func TestTransportWarning(t *testing.T) {
	// Test creating a response with warning
	resp, err := NewResponse("test1", "result with warning")
	if err != nil {
		t.Fatalf("Failed to create response: %v", err)
	}

	// Add a transport warning
	resp.Messages = []LogMessage{
		{
			Level:   WarnWarn,
			Message: "This method is deprecated and will be removed in future versions",
			Details: map[string]interface{}{"alternative": "use_new_method"},
		},
	}

	if len(resp.Messages) == 0 {
		t.Error("Expected Warnings to be set")
	}

	if resp.Messages[0].Level != WarnWarn {
		t.Errorf("Expected warning level 'warn', got '%s'", resp.Messages[0].Level)
	}

	if resp.Messages[0].Message != "This method is deprecated and will be removed in future versions" {
		t.Errorf("Expected warning message 'This method is deprecated and will be removed in future versions', got '%s'", resp.Messages[0].Message)
	}

	// Test JSON marshaling with warning
	data, err := resp.ToJSON()
	if err != nil {
		t.Fatalf("Failed to marshal message with warning: %v", err)
	}

	// Test JSON unmarshaling with warning
	msg, err := FromJSON(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal message with warning: %v", err)
	}

	if len(msg.Messages) == 0 {
		t.Error("Expected Warnings to be preserved after JSON round-trip")
	}

	if msg.Messages[0].Level != WarnWarn {
		t.Errorf("Expected warning level 'warn' after unmarshaling, got '%s'", msg.Messages[0].Level)
	}
}
