package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	jsonlipc "github.com/noodlr-ai/go-jsonl-ipc"
)

func main() {
	// Configure the Python worker
	config := jsonlipc.WorkerConfig{
		PythonPath: "python3",
		ScriptPath: "examples/python_client.py",
		Timeout:    30 * time.Second,
	}

	// Create and start the client
	client := jsonlipc.NewClient(config)

	fmt.Println("Starting Python worker...")
	if err := client.Start(); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	var wgStart sync.WaitGroup
	wgStart.Add(1)

	// Register an event handler
	client.OnEvent("log", func(msg *jsonlipc.Message) {
		fmt.Printf("Python log: %v\n", msg.Params)
	})

	client.OnEvent("ready", func(msg *jsonlipc.Message) {
		fmt.Println("Python worker is ready!")
		wgStart.Done() // Signal that the worker is ready
	})

	wgStart.Wait() // Wait for Python to signal that it's ready

	// Make some RPC calls
	fmt.Println("\nMaking RPC calls...")
	var wg sync.WaitGroup

	// Call ping
	wg.Add(1)
	_, err := client.SendRequestWithTimeoutAndHandler("ping", nil, 2, func(msg *jsonlipc.Message, err error) {
		if err != nil {
			log.Fatalf("Ping failed: %v", err)
		}
		if msg == nil {
			log.Fatalf("Ping response is nil")
			return
		}
		s, ok := msg.Result.(string)
		if !ok {
			log.Fatalf("Ping response is not a string: %v", msg.Result)
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
		s, ok := msg.Result.(float64) // all JSON numbers are float64 when unmarshalled in Go
		if !ok {
			log.Fatalf("Add response is not a float64, got %[1]v (%[1]T) instead", msg.Result)
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
}
