package jsonlipc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	LogDir     string        // Directory for log files
	Timeout    time.Duration // Timeout for process operations
	Env        []string      // Environment variables as "key=value" strings
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
	logger     *log.Logger
	logFile    *os.File
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

	logDir := w.config.LogDir
	// Check if worker.LogDir exists; if not, create it
	if logDir != "" {
		// Convert to absolute path
		absLogDir, err := filepath.Abs(logDir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve log directory path: %w", err)
		}
		logDir = absLogDir

		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create log directory: %w", err)
			}
		}
	} else {
		// If LogDir is not specified, set it to the ScriptPath directory
		absScriptPath, err := filepath.Abs(w.config.ScriptPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve script path: %w", err)
		}
		logDir = filepath.Dir(absScriptPath)
	}

	// Initialize logger
	logFilePath := filepath.Join(logDir, "jsonlipc.log")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	w.logFile = logFile
	w.logger = log.New(w.logFile, "", log.LstdFlags)

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
	stdErrChan := make(chan error, 10)
	go w.handleStderr(stderr, stdErrChan)

	// Note: we are not currently listening for the context to be cancelled; not sure if it is needed
	go func(done chan error) {
		defer close(done)
		err := w.cmd.Wait() // immediately closes stdout/stderr pipes upon return
		// It was not a forced stop; it has exited unexpectedly
		if !w.forcedStop && w.IsRunning() {
			w.sendErrorWithoutWaiting(fmt.Errorf("worker process exited unexpectedly: %w", err), stdErrChan)
			w.cleanup() // Clean up state
			return
		}

		// It has been forced to stop and we are currently waiting for the Wait() signal as part of the Stop() shutdown process
		if w.forcedStop && w.IsRunning() {
			w.done <- err
		}
	}(w.done)

	return stdErrChan, nil
}

// Cleans-up state when stopped
func (w *Worker) cleanup() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.stream = nil
	w.cmd = nil
	w.running = false

	// Close log file if open
	if w.logFile != nil {
		w.logFile.Close()
		w.logFile = nil
		w.logger = nil
	}
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

	// Process hasn't been started yet
	if w.cmd == nil || w.cmd.Process == nil {
		return false
	}

	// ProcessState is only set after Wait() returns
	// So if it's nil, the process is still running
	return w.cmd.ProcessState == nil
}

// Stream returns the communication stream for the worker
func (w *Worker) Stream() *Stream {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.stream
}

// handleStderr reads from stderr and sends errors to the provided channel
// Data sent to stderr is assumed to be error messages from the Python process and are not part of the JSON Lines IPC protocol
// Note: may want to have the channel closed external to this function as part of the worker shutdown process
// Note: stderr is often used as a channel for diagnostic messages, warnings, and non-critical information that is not part of the program's intended output.
func (w *Worker) handleStderr(stderr io.Reader, stdErrChan chan<- error) {
	defer close(stdErrChan)
	scanner := bufio.NewScanner(stderr)
	var errLines []string
	var timer *time.Timer

	// LEFT-OFF: adding a new issue for handling stderr lines without newlines (this requires a custom split function and timeout functionality)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 {
			errLines = append(errLines, line)

			// Start or reset the debounce timer
			if timer == nil {
				timer = time.AfterFunc(100*time.Millisecond, func() {
					err := fmt.Errorf("<--[Engine STDERR]: %s-->", strings.Join(errLines, "\n"))
					w.Log(err.Error())
					w.sendErrorWithoutWaiting(err, stdErrChan)
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
		err := fmt.Errorf("<--[Engine STDERR]: %s-->", strings.Join(errLines, "\n"))
		w.Log(err.Error())
		w.sendErrorWithoutWaiting(err, stdErrChan)
	}

	if err := scanner.Err(); err != nil {
		err := fmt.Errorf("<--[Engine STDERR]: %s-->", strings.Join(errLines, "\n"))
		w.Log(err.Error())
		w.sendErrorWithoutWaiting(err, stdErrChan)
	}
}

func (w *Worker) sendErrorWithoutWaiting(err error, errChan chan<- error) {
	select {
	case errChan <- err:
	default:
		// Channel full, error dropped but shutdown still happens
	}
}

// Log sends a log message to the worker's LogDir
func (w *Worker) Log(message string) {
	w.mutex.RLock()
	logger := w.logger
	w.mutex.RUnlock()

	if logger != nil {
		logger.Println(message)
	}
}

func (w *Worker) ClearLogFile() error {
	w.mutex.RLock()
	logFile := w.logFile
	w.mutex.RUnlock()

	if logFile == nil {
		return fmt.Errorf("log file is not available")
	}

	// Close current log file
	logFile.Close()

	// Truncate the log file
	err := os.Truncate(logFile.Name(), 0)
	if err != nil {
		return fmt.Errorf("failed to truncate log file: %w", err)
	}

	// Reopen the log file for appending
	newLogFile, err := os.OpenFile(logFile.Name(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen log file: %w", err)
	}

	w.mutex.Lock()
	w.logFile = newLogFile
	w.logger = log.New(w.logFile, "", log.LstdFlags)
	w.mutex.Unlock()

	return nil
}

func (w *Worker) ReadLogFile() (string, error) {
	w.mutex.RLock()
	logFile := w.logFile
	w.mutex.RUnlock()

	if logFile == nil {
		return "", fmt.Errorf("log file is not available")
	}

	data, err := os.ReadFile(logFile.Name())
	if err != nil {
		return "", fmt.Errorf("failed to read log file: %w", err)
	}

	return string(data), nil
}
