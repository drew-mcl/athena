package runner

import (
	"context"
	"errors"
)

var ErrCodexNotImplemented = errors.New("codex runner not implemented")

// CodexRunner is a placeholder runner for Codex harnesses.
type CodexRunner struct{}

func (r *CodexRunner) Provider() string {
	return ProviderCodex
}

func (r *CodexRunner) Capabilities() Capabilities {
	return Capabilities{
		JSONInput:       true,
		JSONOutput:      true,
		SessionID:       true,
		Resume:          true,
		ForkSession:     true,
		Plan:            true,
		SkipPermissions: true,
		AllowedTools:    true,
		SystemPrompt:    true,
		MaxTurns:        true,
	}
}

func (r *CodexRunner) Start(ctx context.Context, spec RunSpec) (Session, error) {
	_ = ctx
	_ = spec
	return nil, ErrCodexNotImplemented
}

func (r *CodexRunner) Resume(ctx context.Context, spec ResumeSpec) (Session, error) {
	_ = ctx
	_ = spec
	return nil, ErrCodexNotImplemented
}

func (r *CodexRunner) Attach(ctx context.Context, pid int, opts AttachOptions) (Session, error) {
	return nil, ErrCodexNotImplemented
}
