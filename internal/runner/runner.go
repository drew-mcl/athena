package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderGemini = "gemini"
)

// Capabilities describes what a runner supports.
type Capabilities struct {
	JSONInput       bool `json:"json_input,omitempty"`
	JSONOutput      bool `json:"json_output,omitempty"`
	SessionID       bool `json:"session_id,omitempty"`
	Resume          bool `json:"resume,omitempty"`
	ForkSession     bool `json:"fork_session,omitempty"`
	Plan            bool `json:"plan,omitempty"`
	SkipPermissions bool `json:"skip_permissions,omitempty"`
	AllowedTools    bool `json:"allowed_tools,omitempty"`
	SystemPrompt    bool `json:"system_prompt,omitempty"`
	MaxTurns        bool `json:"max_turns,omitempty"`
}

// RunSpec defines how to start a new session.
type RunSpec struct {
	SessionID       string
	WorkDir         string
	Prompt          string
	Model           string
	PermissionMode  string
	SkipPermissions bool
	AllowedTools    []string
	SystemPrompt    string
	ForkSession     string
	MaxBudgetUSD    float64
	Plan            bool
	GitIdentity     *GitIdentityConfig
	Env             map[string]string
}

type GitIdentityConfig struct {
	AuthorName   string
	AuthorEmail  string
	CoAuthorLine string
}

// ResumeSpec defines how to resume an existing session.
type ResumeSpec struct {
	SessionID       string
	WorkDir         string
	Model           string
	PermissionMode  string
	SkipPermissions bool
	AllowedTools    []string
	SystemPrompt    string
	MaxBudgetUSD    float64
	Plan            bool
}

// Event is a normalized runner event.
type Event struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Timestamp time.Time       `json:"-"`
	Raw       json.RawMessage `json:"raw,omitempty"`
	Usage     *EventUsage     `json:"usage,omitempty"`

	// Result event fields (timing, cost, turns)
	DurationMS    int64   `json:"duration_ms,omitempty"`
	APITimeMS     int64   `json:"api_time_ms,omitempty"`
	CostUSD       float64 `json:"cost_usd,omitempty"`
	NumTurns      int     `json:"num_turns,omitempty"`
	CacheCreation int     `json:"cache_creation,omitempty"`
}

// EventUsage tracks token usage for an event.
type EventUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheReads   int `json:"cache_reads"`
}

// Session represents a running agent session.
type Session interface {
	ID() string
	PID() int
	IsRunning() bool
	ExitCode() int
	SendJSON(msg any) error
	Events() <-chan Event
	Errors() <-chan error
	Done() <-chan struct{}
	Stop() error
}

// Runner starts and resumes sessions for a provider.
type Runner interface {
	Provider() string
	Capabilities() Capabilities
	Start(ctx context.Context, spec RunSpec) (Session, error)
	Resume(ctx context.Context, spec ResumeSpec) (Session, error)
}

// New constructs a runner for the requested provider.
func New(provider string) (Runner, error) {
	switch strings.ToLower(provider) {
	case "", ProviderClaude:
		return &ClaudeRunner{}, nil
	case ProviderCodex:
		return &CodexRunner{}, nil
	case ProviderGemini:
		return &GeminiRunner{}, nil
	default:
		return nil, fmt.Errorf("unknown runner provider: %s", provider)
	}
}
