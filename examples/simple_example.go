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
	client.OnEvent("log", func(msg *jsonlipc.Message) {
		// TODO: this needs to be passed to the UI
		fmt.Printf("Python log: %v\n", msg.Params)
	})

	// Ready event is not being propagated, so a context timeout is occurring
	client.OnEvent("ready", func(msg *jsonlipc.Message) {
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

		s, err := jsonlipc.GetTypedResult[string](msg)
		if err != nil {
			log.Fatalf("Ping response is not a string: %v", msg.Data)
		}

		if s != "pong" {
			log.Fatalf("Unexpected ping response: %v", s)
		}
		fmt.Println("Ping successful:", s)
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
		s, ok := msg.Data.(float64) // all JSON numbers are float64 when unmarshalled in Go
		if !ok {
			log.Fatalf("Add response is not a float64, got %[1]v (%[1]T) instead", msg.Data)
		}
		if s != float64(8) {
			log.Fatalf("Unexpected add response: %v", s)
		}
		fmt.Println("Add successful:", s)
		wg.Done()
	})

	if err != nil {
		log.Fatalf("Add failed: %v", err)
	}

	fmt.Println("Waiting for add response...")
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
		fmt.Println("Shutdown successful:", msg.Data)
		wg.Done()
	})

	if err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}
	wg.Wait()

}
