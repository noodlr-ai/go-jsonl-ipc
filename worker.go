package jsonlipc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
	config     WorkerConfig
	cmd        *exec.Cmd
	stream     *Stream
	ctx        context.Context
	cancel     context.CancelFunc
	mutex      sync.RWMutex
	running    bool
	forcedStop bool
	done       chan error // Used with cmd.Wait() to signal that the process has exited
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
func (w *Worker) Start() (<-chan error, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.running {
		return nil, fmt.Errorf("worker is already running")
	}

	// Build command arguments
	// Use -u flag to force unbuffered binary stdin/stdout
	args := []string{"-u", w.config.ScriptPath}
	args = append(args, w.config.Args...)

	// Create command with context
	w.cmd = exec.CommandContext(w.ctx, w.config.PythonPath, args...)

	// Set working directory if specified
	if w.config.WorkingDir != "" {
		w.cmd.Dir = w.config.WorkingDir
	}

	// Set environment variables if specified
	env := os.Environ()
	if len(w.config.Env) > 0 {
		env = w.config.Env
	}
	// Force Python to use unbuffered I/O for stdin/stdout
	// This prevents buffering issues when Python uses time.sleep()
	env = append(env, "PYTHONUNBUFFERED=1")
	w.cmd.Env = env

	// Set up pipes for stdin, stdout, stderr
	stdin, err := w.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := w.cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Create stream for communication
	w.stream = NewStream(stdout, stdin, w.IsRunning)

	// Start the process
	// Note: this only throws errors if the process fails to launch, so if python.exe launches then err is nil, even if the script python.exe attempts
	// to launch doesn't exist or contains errors
	if err := w.cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Python process: %w", err)
	}

	w.done = make(chan error, 1)
	w.running = true
	w.forcedStop = false

	// Start goroutine to handle stderr
	// Note: this is important for capturing process errors (e.g., when python.exe fails to launch the script because it doesn't exist)
	errChan := make(chan error, 10)
	go w.handleStderr(stderr, errChan)

	// Note: we are not currently listening for the context to be cancelled; not sure if it is needed
	go func(done chan error) {
		defer close(done)
		err := w.cmd.Wait()
		// It was not a forced stop; it has exited unexpectedly
		if !w.forcedStop && w.IsRunning() {
			w.sendErrorWithoutWaiting(fmt.Errorf("worker process exited unexpectedly: %w", err), errChan)
			w.cleanup() // Clean up state
			return
		}

		// It has been forced to stop and we are currently waiting for the Wait() signal as part of the Stop() shutdown process
		if w.forcedStop && w.IsRunning() {
			w.done <- err
		}
	}(w.done)

	return errChan, nil
}

// Cleans-up state when stopped
func (w *Worker) cleanup() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.stream = nil
	w.cmd = nil
	w.running = false
}

// Stop stops the Python worker process
func (w *Worker) Stop() error {

	w.mutex.Lock()
	w.forcedStop = true
	if !w.running {
		w.mutex.Unlock()
		return nil
	}

	// Cancel context to signal shutdown
	w.cancel()
	w.mutex.Unlock()

	// Try graceful shutdown first
	if w.cmd != nil && w.cmd.Process != nil {

		// Windows will not accept os.Interrupt signal for graceful shutdown, send shutdown event instead
		if runtime.GOOS == "windows" {
			req, err := NewRequest("ctrl_01FZ", "shutdown", nil)
			if err == nil {
				w.Stream().WriteMessage(req)
			}
		} else {
			// On Unix-like systems, use SIGINT
			if err := w.cmd.Process.Signal(os.Interrupt); err != nil {
				// If graceful shutdown fails on Unix, force kill
				w.cmd.Process.Kill() // Note: this will cause Wait() to unblock as expected
			}
		}

		select {
		case <-w.done:
			return nil // Process finished gracefully
		case <-time.After(w.config.Timeout):
			// Timeout reached, force kill
			w.cmd.Process.Kill()
			<-w.done // Wait for process to be killed
		}
	}

	w.cleanup() // Clean up state
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
func (w *Worker) handleStderr(stderr io.Reader, errChan chan<- error) {
	scanner := bufio.NewScanner(stderr)
	var errLines []string
	var timer *time.Timer

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 {
			errLines = append(errLines, line)

			// Start or reset the debounce timer
			if timer == nil {
				timer = time.AfterFunc(100*time.Millisecond, func() {
					w.Stop()
					w.sendErrorWithoutWaiting(fmt.Errorf("error received from worker on stderr:\n%s",
						strings.Join(errLines, "\n")), errChan)
					errLines = nil
					timer = nil
				})
			} else {
				timer.Reset(100 * time.Millisecond)
			}
		}
	}

	// Send any remaining accumulated lines
	if timer != nil {
		timer.Stop()
	}
	if len(errLines) > 0 {
		w.Stop()
		w.sendErrorWithoutWaiting(fmt.Errorf("error received from worker on stderr:\n%s",
			strings.Join(errLines, "\n")), errChan)
	}

	if err := scanner.Err(); err != nil {
		w.sendErrorWithoutWaiting(err, errChan)
	}
}

func (w *Worker) sendErrorWithoutWaiting(err error, errChan chan<- error) {
	select {
	case errChan <- err:
	default:
		// Channel full, error dropped but shutdown still happens
	}
}
