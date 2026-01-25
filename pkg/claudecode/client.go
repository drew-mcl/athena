package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/executil"
)

// Process represents a running Claude Code process.
type Process struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderrPipe io.ReadCloser

	events    chan *Event
	errors    chan error
	stderrOut chan string // stderr lines for debugging
	done      chan struct{}

	mu       sync.Mutex
	running  bool
	exitCode int
	pid      int
}

// Spawn starts a new Claude Code process with the given options.
func Spawn(ctx context.Context, opts *SpawnOptions) (*Process, error) {
	args := opts.Args()
	cmd, err := executil.CommandContext(ctx, "claude", args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = opts.WorkDir

	// Inject git identity environment variables if configured.
	if opts.GitIdentity != nil {
		gitEnv := opts.GitIdentity.Env()
		if len(gitEnv) > 0 {
			cmd.Env = append(os.Environ(), gitEnv...)
		}
	}

	// Handle Stdin
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	var stdout io.ReadCloser
	var stderrPipe io.ReadCloser // Only used if not logging to file

	if opts.LogFile != "" {
		// Log to file mode (enables reattachment)
		f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			stdin.Close()
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		// We can close the file in the parent after Start(), but the child process needs it.
		// exec.Cmd handles passing the file descriptor to the child.
		defer f.Close()

		cmd.Stdout = f
		cmd.Stderr = f
	} else {
		// Legacy pipe mode
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			stdin.Close()
			return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		stdout = stdoutPipe

		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			stdin.Close()
			if stdout != nil {
				stdout.Close()
			}
			return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
		}
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		if stdout != nil {
			stdout.Close()
		}
		if stderrPipe != nil {
			stderrPipe.Close()
		}
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// If logging to file, setup the tail reader
	if opts.LogFile != "" {
		// Start reading from the beginning of the session
		tr, err := newTailReader(ctx, opts.LogFile, cmd.Process.Pid)
		if err != nil {
			cmd.Process.Kill()
			return nil, fmt.Errorf("failed to tail log file: %w", err)
		}
		stdout = tr
	}

	p := &Process{
		cmd:        cmd,
		pid:        cmd.Process.Pid,
		stdin:      stdin,
		stdout:     stdout,
		stderrPipe: stderrPipe,
		events:     make(chan *Event, 100),
		errors:     make(chan error, 10),
		stderrOut:  make(chan string, 100),
		done:       make(chan struct{}),
		running:    true,
	}

	go p.readLoop()
	if p.stderrPipe != nil {
		go p.stderrLoop()
	}
	go p.waitLoop()

	// Send initial prompt via stdin if provided
	if opts.Prompt != "" {
		if err := p.SendUserMessage(opts.Prompt); err != nil {
			p.Kill()
			return nil, fmt.Errorf("failed to send initial prompt: %w", err)
		}
	}

	return p, nil
}

// Attach attaches to an existing Claude Code process via its log file.
// Note: Stdin will not be available, so you cannot send new messages.
func Attach(ctx context.Context, pid int, logFile string) (*Process, error) {
	// Verify process exists
	if !isProcessRunning(pid) {
		return nil, fmt.Errorf("process %d is not running", pid)
	}

	// Create tail reader
	tr, err := newTailReader(ctx, logFile, pid)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Seek to end to avoid replaying history
	if _, err := tr.f.Seek(0, io.SeekEnd); err != nil {
		tr.Close()
		return nil, fmt.Errorf("failed to seek log file: %w", err)
	}

	p := &Process{
		cmd:        nil,
		pid:        pid,
		stdin:      nil, // Cannot write to stdin
		stdout:     tr,
		stderrPipe: nil,
		events:     make(chan *Event, 100),
		errors:     make(chan error, 10),
		stderrOut:  make(chan string, 100),
		done:       make(chan struct{}),
		running:    true,
		exitCode:   -1, // Unknown initially
	}

	go p.readLoop()

	// We need a custom wait loop because we don't have p.cmd
	go func() {
		// Poll for process exit
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !isProcessRunning(pid) {
					p.mu.Lock()
					p.running = false
					p.mu.Unlock()
					close(p.done)
					return
				}
			}
		}
	}()

	return p, nil
}

// Events returns a channel of events from the Claude process.
func (p *Process) Events() <-chan *Event {
	return p.events
}

// Errors returns a channel of errors from the process.
func (p *Process) Errors() <-chan error {
	return p.errors
}

// Stderr returns a channel of stderr lines from the process.
func (p *Process) Stderr() <-chan string {
	return p.stderrOut
}

// Done returns a channel that closes when the process exits.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// PID returns the process ID.
func (p *Process) PID() int {
	return p.pid
}

// ExitCode returns the exit code (only valid after Done channel closes).
func (p *Process) ExitCode() int {
	return p.exitCode
}

// IsRunning returns true if the process is still running.
func (p *Process) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// userMessageEnvelope wraps a user message for Claude's stream-json input format.
type userMessageEnvelope struct {
	Type    string      `json:"type"`
	Message userMessage `json:"message"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SendUserMessage sends a user message to Claude in the expected format.
func (p *Process) SendUserMessage(content string) error {
	return p.SendInput(userMessageEnvelope{
		Type: "user",
		Message: userMessage{
			Role:    "user",
			Content: content,
		},
	})
}

// SendInput sends a JSON message to Claude's stdin.
func (p *Process) SendInput(msg any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return fmt.Errorf("process not running")
	}

	if p.stdin == nil {
		return fmt.Errorf("process input not available (attached mode)")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = p.stdin.Write(append(data, '\n'))
	return err
}

// Kill terminates the process.
func (p *Process) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}

	// Attached mode
	if p.pid > 0 {
		if proc, err := os.FindProcess(p.pid); err == nil {
			return proc.Kill()
		}
	}
	return nil
}

// Interrupt sends an interrupt signal to the process.
func (p *Process) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Signal(interruptSignal())
	}

	// Attached mode
	if p.pid > 0 {
		if proc, err := os.FindProcess(p.pid); err == nil {
			return proc.Signal(interruptSignal())
		}
	}
	return nil
}

func (p *Process) readLoop() {
	scanner := bufio.NewScanner(p.stdout)
	// Increase buffer size for large outputs
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		event, err := ParseEvent(line)
		if err != nil {
			// Include raw content in error for debugging
			parseErr := fmt.Errorf("parse error: %w (raw: %s)", err, truncate(string(line), 200))
			select {
			case p.errors <- parseErr:
			default:
			}
			continue
		}

		select {
		case p.events <- event:
		case <-p.done:
			return
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case p.errors <- err:
		default:
		}
	}
}

func (p *Process) stderrLoop() {
	if p.stderrPipe == nil {
		return
	}
	scanner := bufio.NewScanner(p.stderrPipe)
	for scanner.Scan() {
		line := scanner.Text()
		select {
		case p.stderrOut <- line:
		case <-p.done:
			return
		default:
			// Drop if channel full
		}
	}
}

func (p *Process) waitLoop() {
	err := p.cmd.Wait()

	p.mu.Lock()
	p.running = false
	if p.cmd.ProcessState != nil {
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}
	p.mu.Unlock()

	if err != nil {
		select {
		case p.errors <- err:
		default:
		}
	}

	close(p.done)
}

// truncate returns the first n characters of s, with "..." if truncated
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}