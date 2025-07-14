package jsonlipc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// WorkerConfig holds configuration for a Python worker process
type WorkerConfig struct {
	PythonPath string        // Path to Python executable (default: "python3")
	ScriptPath string        // Path to Python script to execute
	Args       []string      // Additional arguments for the Python script
	WorkingDir string        // Working directory for the process
	Timeout    time.Duration // Timeout for process operations
	Env        []string      // Environment variables
}

// Worker represents a managed Python process for IPC communication
type Worker struct {
	config  WorkerConfig
	cmd     *exec.Cmd
	stream  *Stream
	ctx     context.Context
	cancel  context.CancelFunc
	mutex   sync.RWMutex
	running bool
}

// NewWorker creates a new worker with the given configuration
func NewWorker(config WorkerConfig) *Worker {
	if config.PythonPath == "" {
		config.PythonPath = "python3"
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start starts the Python worker process
func (w *Worker) Start() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.running {
		return fmt.Errorf("worker is already running")
	}

	// Build command arguments
	args := []string{w.config.ScriptPath}
	args = append(args, w.config.Args...)

	// Create command with context
	w.cmd = exec.CommandContext(w.ctx, w.config.PythonPath, args...)

	// Set working directory if specified
	if w.config.WorkingDir != "" {
		w.cmd.Dir = w.config.WorkingDir
	}

	// Set environment variables if specified
	if len(w.config.Env) > 0 {
		w.cmd.Env = w.config.Env
	}

	// Set up pipes for stdin, stdout, stderr
	stdin, err := w.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := w.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Create stream for communication
	w.stream = NewStream(stdout, stdin)

	// Start the process
	if err := w.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Python process: %w", err)
	}

	w.running = true

	// Start goroutine to handle stderr
	go w.handleStderr(stderr)

	return nil
}

// Stop stops the Python worker process
func (w *Worker) Stop() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if !w.running {
		return nil
	}

	// Cancel context to signal shutdown
	w.cancel()

	// Try graceful shutdown first
	if w.cmd != nil && w.cmd.Process != nil {
		if err := w.cmd.Process.Signal(os.Interrupt); err != nil {
			// If graceful shutdown fails, force kill
			w.cmd.Process.Kill()
		}

		// Wait for process to finish with timeout
		done := make(chan error, 1)
		go func() {
			done <- w.cmd.Wait()
		}()

		select {
		case <-done:
			// Process finished
		case <-time.After(w.config.Timeout):
			// Timeout reached, force kill
			w.cmd.Process.Kill()
			<-done // Wait for process to be killed
		}
	}

	w.running = false
	w.cmd = nil
	w.stream = nil

	return nil
}

// IsRunning returns whether the worker process is currently running
func (w *Worker) IsRunning() bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.running
}

// Stream returns the communication stream for the worker
func (w *Worker) Stream() *Stream {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.stream
}

// handleStderr reads from stderr and logs errors
func (w *Worker) handleStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		// For now, we'll just ignore stderr output
		// In a more advanced implementation, you might want to log this
		// or provide a callback mechanism
		_ = scanner.Text()
	}
}
