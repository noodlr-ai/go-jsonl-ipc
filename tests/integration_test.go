package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	jsonlipc "github.com/noodlr-ai/go-jsonl-ipc"
)

// getPythonPath returns the Python executable path to use for tests
func getPythonPath() string {
	// Try to use pyenv Python first
	homeDir, err := os.UserHomeDir()
	if err == nil {
		pyenvPath := fmt.Sprintf("%s/.pyenv/versions/3.13.5/bin/python", homeDir)
		if _, err := os.Stat(pyenvPath); err == nil {
			return pyenvPath
		}
	}

	// Fall back to system Python
	return "python3"
}

func setupClient() *jsonlipc.Client {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}
	return jsonlipc.NewClient(config)
}

// TestIntegrationPing tests the basic ping functionality
func TestIntegrationPing(t *testing.T) {
	client := setupClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	// Set up ready handler
	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	// Wait for ready event
	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly on startup: %v", err)
	case <-readyChan:
		// Worker is ready
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker to be ready: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Call ping
	_, err = client.SendRequestWithTimeoutAndHandler("ping", nil, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Ping failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Ping response is nil")
			return
		}

		// Unmarshal envelope
		env, err := msg.UnmarshalEnvelope()
		if err != nil {
			t.Errorf("Failed to unmarshal envelope: %v", err)
			return
		}

		resp, ok := env.(*jsonlipc.ResultEnvelope)
		if !ok {
			t.Errorf("Unexpected envelope type: %T", env)
			return
		}

		type PingResponse struct {
			Response string `json:"response"`
		}

		var pingResp PingResponse
		if err := resp.UnmarshalDataPayload(&pingResp); err != nil {
			t.Errorf("Failed to unmarshal ping response: %v", err)
			return
		}

		if pingResp.Response != "pong" {
			t.Errorf("Unexpected ping response: %v", pingResp.Response)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send ping request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationAdd tests the add method
func TestIntegrationAdd(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	// Wait for ready event
	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
		// Worker is ready
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Call add method
	_, err = client.SendRequestWithTimeoutAndHandler("add", map[string]int{"a": 5, "b": 3}, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Add failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Add response is nil")
			return
		}

		type AddResponse float64

		var addResp AddResponse
		if err := msg.UnmarshalDataPayload(&addResp); err != nil {
			t.Errorf("Failed to unmarshal add response: %v", err)
			return
		}
		if addResp != 8 {
			t.Errorf("Expected add response 8, got: %v", addResp)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send add request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationMultiply tests the multiply method
func TestIntegrationMultiply(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Call multiply method with default parameters
	_, err = client.SendRequestWithTimeoutAndHandler("multiply", map[string]float64{"a": 4.5, "b": 2.0}, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Multiply failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Multiply response is nil")
			return
		}

		var multiplyResp float64
		if err := msg.UnmarshalDataPayload(&multiplyResp); err != nil {
			t.Errorf("Failed to unmarshal multiply response: %v", err)
			return
		}
		expected := 9.0
		if multiplyResp != expected {
			t.Errorf("Expected multiply response %.1f, got: %.1f", expected, multiplyResp)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send multiply request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationDivide tests the divide method including error handling
func TestIntegrationDivide(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup

	// Test successful division
	wg.Add(1)
	_, err = client.SendRequestWithTimeoutAndHandler("divide", map[string]float64{"a": 10.0, "b": 2.0}, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Divide failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Divide response is nil")
			return
		}

		var divideResp float64
		if err := msg.UnmarshalDataPayload(&divideResp); err != nil {
			t.Errorf("Failed to unmarshal divide response: %v", err)
			return
		}
		expected := 5.0
		if divideResp != expected {
			t.Errorf("Expected divide response %.1f, got: %.1f", expected, divideResp)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send divide request: %v", err)
	}

	wg.Wait()

	// Note: Division by zero error handling test removed as the Python worker
	// may handle this differently depending on implementation
}

func TestIntegrationDivideByZeroError(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup

	// Test successful division
	wg.Add(1)
	_, err = client.SendRequestWithTimeoutAndHandler("divide", map[string]float64{"a": 10.0, "b": 0.0}, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Divide failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Divide response is nil")
			return
		}
		if msg.Type != jsonlipc.MessageTypeResponse {
			t.Errorf("Expected response message type, got: %v", msg.Type)
		}

		// get the envelope from the response
		env, err := msg.UnmarshalEnvelope()
		if err != nil {
			t.Errorf("Failed to unmarshal envelope: %v", err)
			return
		}

		// envelope should be an ErrorEnvelope
		divideErr, ok := env.(*jsonlipc.ErrorEnvelope)
		if !ok {
			t.Errorf("Unexpected envelope type: %T", env)
		}

		if divideErr.Error.Code != "zeroDivisionError" {
			t.Errorf("Expected error code %s, got: %s", "zeroDivisionError", divideErr.Error.Code)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send divide request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationProgress tests progress notifications
func TestIntegrationProgress(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	numSteps := 5
	var progressCount atomic.Int32
	var finalResponse atomic.Bool

	_, err = client.SendRequestWithTimeoutAndHandler("progress",
		map[string]any{
			"steps": numSteps,
			"delay": 0.1,
		},
		5, // 5 second timeout
		func(msg *jsonlipc.Message, err error) {
			if err != nil {
				t.Errorf("Progress handler failed: %v", err)
				wg.Done()
				return
			}
			if msg == nil {
				t.Error("Progress handler response is nil")
				wg.Done()
				return
			}

			switch msg.Type {
			case jsonlipc.MessageTypeNotification:
				env, err := msg.UnmarshalEnvelope()
				if err != nil {
					t.Errorf("Failed to unmarshal envelope: %v", err)
					return
				}

				progress, ok := env.(*jsonlipc.ProgressEnvelope)
				if !ok {
					t.Errorf("Unexpected envelope type: %T", env)
					return
				}

				progressCount.Add(1)
				t.Logf("Progress: %.1f%% (%s) - %s",
					progress.Progress.Ratio*100,
					progress.Progress.Stage,
					progress.Progress.Message)

			case jsonlipc.MessageTypeResponse:
				finalResponse.Store(true)

				type ProgressResponse struct {
					Status     string `json:"status"`
					TotalSteps int    `json:"total_steps"`
				}

				var progressResp ProgressResponse
				if err := msg.UnmarshalDataPayload(&progressResp); err != nil {
					t.Errorf("Failed to unmarshal progress response: %v", err)
					wg.Done()
					return
				}

				if progressResp.TotalSteps != numSteps {
					t.Errorf("Expected total_steps %d, got: %d", numSteps, progressResp.TotalSteps)
				}

				t.Logf("Progress handler completed: %s (total steps: %d)",
					progressResp.Status,
					progressResp.TotalSteps)
				wg.Done()

			default:
				t.Errorf("Unexpected message type: %v", msg.Type)
				wg.Done()
			}
		})

	if err != nil {
		t.Fatalf("Progress handler call failed: %v", err)
	}

	wg.Wait()

	// Verify we received progress updates
	if progressCount.Load() < int32(numSteps) {
		t.Errorf("Expected at least %d progress updates, got: %d", numSteps, progressCount.Load())
	}

	if !finalResponse.Load() {
		t.Error("Expected final response, but didn't receive one")
	}
}

// TestIntegrationLog tests log message handling
func TestIntegrationLog(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})
	var logCount atomic.Int32

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	// Set up log handler
	client.OnNotification("log", func(msg *jsonlipc.Message) {
		logCount.Add(1)
		t.Logf("Received log notification: %v", msg.Params)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	_, err = client.SendRequestWithTimeoutAndHandler("log", nil, 2, func(msg *jsonlipc.Message, err error) {
		if err != nil {
			t.Errorf("Log handler failed: %v", err)
			wg.Done()
			return
		}
		if msg == nil {
			t.Error("Log handler response is nil")
			wg.Done()
			return
		}

		// Only process actual response messages, skip notifications
		if msg.Type != jsonlipc.MessageTypeResponse {
			return
		}

		type LogResponse struct {
			Status string `json:"status"`
			Count  int    `json:"count"`
		}

		var logResp LogResponse
		if err := msg.UnmarshalDataPayload(&logResp); err != nil {
			t.Errorf("Failed to unmarshal log response: %v", err)
			wg.Done()
			return
		}

		if logResp.Status != "logs_sent" {
			t.Errorf("Expected status 'logs_sent', got: %s", logResp.Status)
		}
		wg.Done()
	})

	if err != nil {
		t.Fatalf("Failed to send log request: %v", err)
	}

	wg.Wait()

	// Give some time for log notifications to arrive
	time.Sleep(100 * time.Millisecond)

	// We should have received at least some log notifications
	if logCount.Load() == 0 {
		t.Error("Expected to receive log notifications, but got none")
	}
}

// TestIntegrationEcho tests the echo handler
func TestIntegrationEcho(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	testParams := map[string]interface{}{
		"message": "hello world",
		"number":  42,
	}

	_, err = client.SendRequestWithTimeoutAndHandler("echo", testParams, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Echo failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Echo response is nil")
			return
		}

		type EchoResponse struct {
			Echo map[string]interface{} `json:"echo"`
		}

		var echoResp EchoResponse
		if err := msg.UnmarshalDataPayload(&echoResp); err != nil {
			t.Errorf("Failed to unmarshal echo response: %v", err)
			return
		}

		if echoResp.Echo["message"] != testParams["message"] {
			t.Errorf("Expected message '%v', got: '%v'", testParams["message"], echoResp.Echo["message"])
		}
	})

	if err != nil {
		t.Fatalf("Failed to send echo request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationShutdown tests graceful shutdown
func TestIntegrationShutdown(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	_, err = client.SendRequestWithTimeoutAndHandler("shutdown", nil, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Shutdown response is nil")
			return
		}

		type ShutdownResponse struct {
			Status string `json:"status"`
		}

		var shutdownResp ShutdownResponse
		if err := msg.UnmarshalDataPayload(&shutdownResp); err != nil {
			t.Errorf("Failed to unmarshal shutdown response: %v", err)
			return
		}

		if shutdownResp.Status != "shutting down" {
			t.Errorf("Expected status 'shutting down', got: %s", shutdownResp.Status)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send shutdown request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationNoop tests handler that returns None
func TestIntegrationNoop(t *testing.T) {
	client := setupClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	_, err = client.SendRequestWithTimeoutAndHandler("noop", nil, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		if err != nil {
			t.Errorf("Noop failed: %v", err)
			return
		}
		if msg == nil {
			t.Error("Noop response is nil")
			return
		}

		// Noop returns None/null, so we should get a null result
		var result interface{}
		if err := msg.UnmarshalDataPayload(&result); err != nil {
			t.Errorf("Failed to unmarshal noop response: %v", err)
			return
		}

		// Result should be nil for None/null
		if result != nil {
			t.Logf("Noop returned: %v (type: %T)", result, result)
		}
	})

	if err != nil {
		t.Fatalf("Failed to send noop request: %v", err)
	}

	wg.Wait()
}

// TestIntegrationInvalidMethod tests calling a non-existent method
func TestIntegrationInvalidMethod(t *testing.T) {
	client := setupClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	_, err = client.SendRequestWithTimeoutAndHandler("invalid_method", nil, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		assert.Equal(t, nil, err)
		assert.Equal(t, msg.Type, jsonlipc.MessageTypeResponse)

		// get the envelope from the response
		env, err := msg.UnmarshalEnvelope()
		assert.Equal(t, nil, err)

		// envelope should be an ErrorEnvelope
		divideErr, ok := env.(*jsonlipc.ErrorEnvelope)
		assert.Equal(t, true, ok)

		assert.EqualValues(t, "methodNotFound", divideErr.Error.Code)
	})

	assert.Equal(t, nil, err)
	wg.Wait()
}

// TestIntegrationMultipleConcurrentRequests tests multiple concurrent requests
func TestIntegrationMultipleConcurrentRequests(t *testing.T) {
	config := jsonlipc.WorkerConfig{
		PythonPath: getPythonPath(),
		ScriptPath: "../examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	client := jsonlipc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	numRequests := 10

	// Send multiple concurrent add requests
	for i := range numRequests {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			a := index
			b := index * 2
			expected := a + b

			_, err := client.SendRequestWithTimeoutAndHandler("add",
				map[string]int{"a": a, "b": b},
				5,
				func(msg *jsonlipc.Message, err error) {
					if err != nil {
						t.Errorf("Request %d failed: %v", index, err)
						return
					}
					if msg == nil {
						t.Errorf("Request %d response is nil", index)
						return
					}

					var result float64
					if err := msg.UnmarshalDataPayload(&result); err != nil {
						t.Errorf("Request %d: Failed to unmarshal response: %v", index, err)
						return
					}

					if int(result) != expected {
						t.Errorf("Request %d: Expected %d, got: %.0f", index, expected, result)
					}
				})

			if err != nil {
				t.Errorf("Request %d: Failed to send: %v", index, err)
			}
		}(i)
	}

	wg.Wait()
}

func TestIntegrationStderrHandling(t *testing.T) {
	client := setupClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Set up a listener for stderr errors
	var stderrError error
	go func() {
		defer wg.Done()
		select {
		case err := <-errChan:
			if err != nil && strings.Contains(err.Error(), "error received from worker on stderr") {
				stderrError = err
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for stderr error")
		}
	}()

	testParams := map[string]any{
		"message": "This is a test error message",
	}

	// Send request that will write to stderr
	_, err = client.SendRequestWithTimeoutAndHandler("stderr", testParams, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		assert.Equal(t, nil, err)
		assert.Equal(t, msg.Type, jsonlipc.MessageTypeResponse)

		// get the envelope from the response
		env, err := msg.UnmarshalEnvelope()
		assert.Equal(t, nil, err)

		// envelope should be an ResultEnvelope
		// Note: the error is captured via stderr, so we expect a ResultEnvelope here
		result, ok := env.(*jsonlipc.ResultEnvelope)
		assert.Equal(t, true, ok)

		var resultData any
		if err := result.UnmarshalDataPayload(&resultData); err != nil {
			t.Errorf("Failed to unmarshal stderr response data: %v", err)
			return
		}

		assert.Nil(t, resultData)
	})

	if err != nil {
		t.Fatalf("Failed to send stderr_test request: %v", err)
	}

	wg.Wait()

	// Verify we received the stderr error
	if stderrError == nil {
		t.Error("Expected to receive stderr error but got none")
	} else {
		assert.Contains(t, stderrError.Error(), "This is a test error message")
	}
}

// TestIntegrationStderrHandlingMultiline tests stderr handling with multiline messages (debounce)
func TestIntegrationStderrHandlingMultiline(t *testing.T) {
	client := setupClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Set up a listener for stderr errors
	var stderrError error
	go func() {
		defer wg.Done()
		select {
		case err := <-errChan:
			if err != nil && strings.Contains(err.Error(), "Error line") {
				stderrError = err
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for stderr error")
		}
	}()

	testParams := map[string]any{
		"message": "This is a test error message",
	}

	// Send request that will write to stderr
	_, err = client.SendRequestWithTimeoutAndHandler("stderr_multiline", testParams, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		assert.Equal(t, nil, err)
		assert.Equal(t, msg.Type, jsonlipc.MessageTypeResponse)

		// get the envelope from the response
		env, err := msg.UnmarshalEnvelope()
		assert.Equal(t, nil, err)

		// envelope should be an ResultEnvelope
		// Note: the error is captured via stderr, so we expect a ResultEnvelope here
		result, ok := env.(*jsonlipc.ResultEnvelope)
		assert.Equal(t, true, ok)

		var resultData any
		if err := result.UnmarshalDataPayload(&resultData); err != nil {
			t.Errorf("Failed to unmarshal stderr response data: %v", err)
			return
		}

		assert.Nil(t, resultData)
	})

	if err != nil {
		t.Fatalf("Failed to send stderr_test request: %v", err)
	}

	wg.Wait()

	// Verify we received the stderr error
	if stderrError == nil {
		t.Error("Expected to receive stderr error but got none")
	} else {
		assert.True(t, strings.Count(stderrError.Error(), "\n") >= 3)
	}
}

func TestIntegrationStdoutMalformedHandling(t *testing.T) {
	client := setupClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		close(readyChan)
	})

	errChan, err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start Python worker: %v", err)
	}
	defer client.Stop()

	select {
	case err := <-errChan:
		t.Fatalf("Worker process exited unexpectedly: %v", err)
	case <-readyChan:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for worker: %v", ctx.Err())
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Set up a listener for stderr errors
	var stderrError error
	go func() {
		defer wg.Done()
		select {
		case err := <-errChan:
			if err != nil && strings.Contains(err.Error(), "error received from worker on stderr") {
				stderrError = err
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for stderr error")
		}
	}()

	// Send request that will write to stderr
	_, err = client.SendRequestWithTimeoutAndHandler("stdout_malformed", nil, 2, func(msg *jsonlipc.Message, err error) {
		defer wg.Done()

		assert.Equal(t, nil, err)
		assert.Equal(t, msg.Type, jsonlipc.MessageTypeResponse)

		// get the envelope from the response
		env, err := msg.UnmarshalEnvelope()
		assert.Equal(t, nil, err)

		// envelope should be an ResultEnvelope
		// Note: the error is captured via stderr, so we expect a ResultEnvelope here
		result, ok := env.(*jsonlipc.ResultEnvelope)
		assert.Equal(t, true, ok)

		var resultData any
		if err := result.UnmarshalDataPayload(&resultData); err != nil {
			t.Errorf("Failed to unmarshal stderr response data: %v", err)
			return
		}

		assert.Nil(t, resultData)
	})

	if err != nil {
		t.Fatalf("Failed to send stderr_test request: %v", err)
	}

	wg.Wait()

	// Verify we received the stderr error
	if stderrError == nil {
		t.Error("Expected to receive stderr error but got none")
	} else {
		assert.Contains(t, stderrError.Error(), "This is a test error message")
	}
}
