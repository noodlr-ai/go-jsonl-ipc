package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	jsonlipc "github.com/noodlr-ai/go-jsonl-ipc"
)

func main() {
	// Configure the Python worker
	homeDir, osErr := os.UserHomeDir()
	if osErr != nil {
		log.Fatalf("Failed to get user home directory: %v", osErr)
	}
	pythonPath := fmt.Sprintf("%s/.pyenv/versions/3.13.5/bin/python", homeDir)

	config := jsonlipc.WorkerConfig{
		PythonPath: pythonPath,
		ScriptPath: "examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	// Create and start the client
	client := jsonlipc.NewClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyChan := make(chan struct{})

	fmt.Println("Starting Python worker...")
	// Set up event handlers
	client.OnNotification("log", func(msg *jsonlipc.Message) {
		// TODO: this needs to be passed to the UI
		fmt.Printf("Python log: %v\n", msg.Params)
	})

	// Ready event is not being propagated, so a context timeout is occurring
	client.OnNotification("ready", func(msg *jsonlipc.Message) {
		fmt.Println("Ready event received")
		close(readyChan)
	})

	errChan, err := client.Start()
	defer client.Stop()

	if err != nil {
		fmt.Println("failed to start Python worker: %w", err)
		return
	}

	// Block until the engine is ready, startup error occurs, or context is cancelled
	select {
	case err := <-errChan:
		fmt.Println("worker process exited unexpectedly on startup: %w", err)
		return
	case <-readyChan:
		fmt.Println("Ready event received")
	case <-ctx.Done():
		fmt.Println("Context timedout before engine was ready")
		// TODO: handle this more gracefully with messages back to the UI
		client.Stop()
		fmt.Println(ctx.Err())
		return
	}

	// Make some RPC calls
	fmt.Println("\nMaking RPC calls...")
	var wg sync.WaitGroup

	// Call ping
	wg.Add(1)
	_, err = client.SendRequestWithTimeoutAndHandler("ping", nil, 2, func(msg *jsonlipc.Message, err error) {
		if err != nil {
			log.Fatalf("Ping failed: %v", err)
		}
		if msg == nil {
			log.Fatalf("Ping response is nil")
			return
		}

		// Use the UnmarshalEnvelope method to get the specific envelope type
		env, err := msg.UnmarshalEnvelope()
		if err != nil {
			log.Fatalf("Failed to unmarshal envelope: %v", err)
		}

		resp, ok := env.(*jsonlipc.ResultEnvelope)
		if !ok {
			log.Fatalf("Unexpected envelope type: %T", env)
		}

		type PingResponse struct {
			Response string `json:"response"`
		}

		var pingResp PingResponse
		if err := resp.UnmarshalDataPayload(&pingResp); err != nil { // could use msg.UnmarshalDataPayload if we wanted to
			log.Fatalf("Failed to unmarshal ping response: %v", err)
		}

		if pingResp.Response != "pong" {
			log.Fatalf("Unexpected ping response: %v", pingResp.Response)
		}

		fmt.Println("Ping successful:", pingResp.Response)
		wg.Done()
	})

	if err != nil {
		log.Fatalf("Ping failed: %v", err)
	}

	fmt.Println("Waiting for ping response...")
	wg.Wait()

	// Call add method
	wg.Add(1)
	_, err = client.SendRequestWithTimeoutAndHandler("add", map[string]int{"a": 5, "b": 3}, 2, func(msg *jsonlipc.Message, err error) {
		if err != nil {
			log.Fatalf("Add failed: %v", err)
		}
		if msg == nil {
			log.Fatalf("Add response is nil")
			return
		}

		// Use the UnmarshalDataPayload method to get the specific payload data without the envelope
		type AddResponse float64

		var addResp AddResponse
		if err := msg.UnmarshalDataPayload(&addResp); err != nil {
			log.Fatalf("Failed to unmarshal add response: %v", err)
		}
		if addResp != 8 {
			log.Fatalf("Unexpected add response: %v", addResp)
		}
		fmt.Println("Add successful:", addResp)
		wg.Done()
	})

	if err != nil {
		log.Fatalf("Add failed: %v", err)
	}

	fmt.Println("Waiting for add response...")
	wg.Wait()

	// Call handler with progress events
	fmt.Println("Testing progress handler...")
	wg.Add(1)

	num_steps, num_progress_notifications := 10, 10

	// Set up progress notification handler

	client.OnNotification("progress", func(msg *jsonlipc.Message) {
		num_progress_notifications--
	})

	_, err = client.SendRequestWithTimeoutAndHandler("progress",
		map[string]any{
			"steps": num_steps,
			"delay": 0.2,
		},
		5, // 5 second timeout
		func(msg *jsonlipc.Message, err error) {
			if err != nil {
				log.Fatalf("Progress handler failed: %v", err)
			}
			if msg == nil {
				log.Fatalf("Progress handler response is nil")
				return
			}

			switch msg.Type {
			case jsonlipc.MessageTypeNotification:
				// Use the UnmarshalEnvelope method to get the specific envelope type
				env, err := msg.UnmarshalEnvelope()
				if err != nil {
					log.Fatalf("Failed to unmarshal envelope: %v", err)
				}

				progress, ok := env.(*jsonlipc.ProgressEnvelope)
				if !ok {
					log.Fatalf("Unexpected envelope type: %T", env)
				}

				fmt.Printf("Progress: %.1f%% (%s) - %s\n",
					progress.Progress.Ratio*100,
					progress.Progress.Stage,
					progress.Progress.Message)
				num_steps = num_steps - 1
			case jsonlipc.MessageTypeResponse:

				if num_steps != 0 {
					log.Fatalf("Expected more progress steps, but got final response early")
				}

				if num_progress_notifications != 0 {
					log.Fatalf("Expected progress notifications to be 0, but got %d", num_steps-num_progress_notifications)
				}

				// Final response received
				type ProgressResponse struct {
					Result struct {
						Status     string `json:"status"`
						TotalSteps int    `json:"total_steps"`
					} `json:"result"`
				}

				var progressResp ProgressResponse
				if err := msg.UnmarshalDataPayload(&progressResp); err != nil {
					log.Fatalf("Failed to unmarshal progress response: %v", err)
				}

				fmt.Printf("Progress handler completed: %s (total steps: %d)\n",
					progressResp.Result.Status,
					progressResp.Result.TotalSteps)
				wg.Done()
			default:
				log.Fatalf("Unexpected message type: %v", msg.Type)
			}
		})

	if err != nil {
		log.Fatalf("Progress handler call failed: %v", err)
	}

	fmt.Println("Waiting for progress handler to complete...")
	wg.Wait()

	wg.Add(1)
	_, err = client.SendRequestWithTimeoutAndHandler("shutdown", nil, 0, func(msg *jsonlipc.Message, err error) {
		if err != nil {
			log.Fatalf("Shutdown failed: %v", err)
		}
		if msg == nil {
			log.Fatalf("Shutdown response is nil")
			return
		}

		var ShutdownResponse struct {
			Status string `json:"status"`
		}

		if err := msg.UnmarshalDataPayload(&ShutdownResponse); err != nil {
			log.Fatalf("Failed to unmarshal shutdown response: %v", err)
		}

		fmt.Println("Shutdown successful:", ShutdownResponse.Status)
		wg.Done()
	})

	if err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}
	wg.Wait()

}
