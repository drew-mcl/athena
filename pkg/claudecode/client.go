package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Process represents a running Claude Code process.
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	events chan *Event
	errors chan error
	done   chan struct{}

	mu       sync.Mutex
	running  bool
	exitCode int
}

// Spawn starts a new Claude Code process with the given options.
func Spawn(ctx context.Context, opts *SpawnOptions) (*Process, error) {
	args := opts.Args()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = opts.WorkDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	p := &Process{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		events:  make(chan *Event, 100),
		errors:  make(chan error, 10),
		done:    make(chan struct{}),
		running: true,
	}

	go p.readLoop()
	go p.waitLoop()

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
		event, err := ParseEvent(scanner.Bytes())
		if err != nil {
			select {
			case p.errors <- err:
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
