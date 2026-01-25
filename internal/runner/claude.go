package runner

import (
	"context"

	"github.com/drewfead/athena/pkg/claudecode"
)

// ClaudeRunner implements Runner using Claude Code CLI.
type ClaudeRunner struct{}

func (r *ClaudeRunner) Provider() string {
	return ProviderClaude
}

func (r *ClaudeRunner) Capabilities() Capabilities {
	return Capabilities{
		JSONInput:    true,
		JSONOutput:   true,
		SessionID:    true,
		Resume:       true,
		ForkSession:  true,
		Plan:         true,
		AllowedTools: true,
		SystemPrompt: true,
		MaxTurns:     true,
	}
}

func (r *ClaudeRunner) Start(ctx context.Context, spec RunSpec) (Session, error) {
	opts := buildClaudeOptions(spec, false)
	proc, err := claudecode.Spawn(ctx, opts)
	if err != nil {
		return nil, err
	}
	return newClaudeSession(proc, opts.SessionID), nil
}

func (r *ClaudeRunner) Resume(ctx context.Context, spec ResumeSpec) (Session, error) {
	opts := buildClaudeOptions(RunSpec{
		SessionID:      spec.SessionID,
		WorkDir:        spec.WorkDir,
		Model:          spec.Model,
		PermissionMode: spec.PermissionMode,
		AllowedTools:   spec.AllowedTools,
		SystemPrompt:   spec.SystemPrompt,
		MaxBudgetUSD:   spec.MaxBudgetUSD,
		Plan:           spec.Plan,
	}, true)
	proc, err := claudecode.Spawn(ctx, opts)
	if err != nil {
		return nil, err
	}
	return newClaudeSession(proc, opts.SessionID), nil
}

type claudeSession struct {
	id     string
	proc   *claudecode.Process
	events chan Event
}

func newClaudeSession(proc *claudecode.Process, sessionID string) *claudeSession {
	s := &claudeSession{
		id:     sessionID,
		proc:   proc,
		events: make(chan Event, 100),
	}
	go s.forwardEvents()
	return s
}

func (s *claudeSession) ID() string {
	return s.id
}

func (s *claudeSession) PID() int {
	return s.proc.PID()
}

func (s *claudeSession) IsRunning() bool {
	return s.proc.IsRunning()
}

func (s *claudeSession) ExitCode() int {
	return s.proc.ExitCode()
}

func (s *claudeSession) SendJSON(msg any) error {
	return s.proc.SendInput(msg)
}

func (s *claudeSession) Events() <-chan Event {
	return s.events
}

func (s *claudeSession) Errors() <-chan error {
	return s.proc.Errors()
}

func (s *claudeSession) Done() <-chan struct{} {
	return s.proc.Done()
}

func (s *claudeSession) Stop() error {
	return s.proc.Kill()
}

func (s *claudeSession) forwardEvents() {
	for ev := range s.proc.Events() {
		if ev == nil {
			continue
		}
		runnerEvent := Event{
			Type:      string(ev.Type),
			Subtype:   ev.Subtype,
			SessionID: ev.SessionID,
			Content:   ev.Content,
			Name:      ev.Name,
			Input:     ev.Input,
			Timestamp: ev.Timestamp,
		}

		// Forward usage data from result events
		if ev.Usage != nil {
			runnerEvent.Usage = &EventUsage{
				InputTokens:  ev.Usage.InputTokens,
				OutputTokens: ev.Usage.OutputTokens,
				CacheReads:   ev.Usage.CacheReadInputTokens,
			}
		}

		// Forward timing and cost data
		if ev.Type == claudecode.EventTypeResult {
			runnerEvent.DurationMS = ev.DurationMS
			runnerEvent.APITimeMS = ev.APITimeMS
			runnerEvent.CostUSD = ev.CostUSD
			runnerEvent.NumTurns = ev.NumTurns
			runnerEvent.CacheCreation = 0
			if ev.Usage != nil {
				runnerEvent.CacheCreation = ev.Usage.CacheCreationInputTokens
			}
		}

		s.events <- runnerEvent
	}
	close(s.events)
}

func buildClaudeOptions(spec RunSpec, resume bool) *claudecode.SpawnOptions {
	opts := &claudecode.SpawnOptions{
		SessionID:      spec.SessionID,
		WorkDir:        spec.WorkDir,
		Prompt:         spec.Prompt,
		Model:          spec.Model,
		PermissionMode: spec.PermissionMode,
		AllowedTools:   spec.AllowedTools,
		SystemPrompt:   spec.SystemPrompt,
		ForkSession:    spec.ForkSession,
		MaxBudgetUSD:   spec.MaxBudgetUSD,
		Resume:         resume,
	}

	// Map git identity from runner spec to claudecode options
	if spec.GitIdentity != nil {
		opts.GitIdentity = &claudecode.GitIdentityConfig{
			AuthorName:   spec.GitIdentity.AuthorName,
			AuthorEmail:  spec.GitIdentity.AuthorEmail,
			CoAuthorLine: spec.GitIdentity.CoAuthorLine,
		}
	}

	if opts.PermissionMode == "" && spec.Plan {
		opts.PermissionMode = "plan"
	}
	if resume {
		opts.Prompt = ""
	}

	return opts
}
