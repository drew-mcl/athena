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
)

// Process represents a running Claude Code process.
type Process struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderrPipe io.ReadCloser

	events    chan *Event
	errors    chan error
	stderrOut chan string // stderr lines for debugging
	done      chan struct{}

	mu       sync.Mutex
	running  bool
	exitCode int
}

// Spawn starts a new Claude Code process with the given options.
func Spawn(ctx context.Context, opts *SpawnOptions) (*Process, error) {
	args := opts.Args()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = opts.WorkDir

	// Inject git identity environment variables if configured
	if opts.GitIdentity != nil {
		gitEnv := opts.GitIdentity.Env()
		if len(gitEnv) > 0 {
			// Start with current environment, then add git identity vars
			cmd.Env = append(os.Environ(), gitEnv...)
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	p := &Process{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderrPipe: stderr,
		events:     make(chan *Event, 100),
		errors:     make(chan error, 10),
		stderrOut:  make(chan string, 100),
		done:       make(chan struct{}),
		running:    true,
	}

	go p.readLoop()
	go p.stderrLoop()
	go p.waitLoop()

	// Send initial prompt via stdin if provided (required for stream-json input)
	if opts.Prompt != "" {
		if err := p.SendUserMessage(opts.Prompt); err != nil {
			p.Kill()
			return nil, fmt.Errorf("failed to send initial prompt: %w", err)
		}
	}

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
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
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
// Format: {"type":"user","message":{"role":"user","content":"..."}}
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

	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
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

	if p.cmd.Process != nil {
		return p.cmd.Process.Signal(interruptSignal())
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
