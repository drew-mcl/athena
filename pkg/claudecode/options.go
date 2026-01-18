package claudecode

import (
	"strconv"
	"strings"
)

// SpawnOptions configures how to spawn a Claude Code process.
type SpawnOptions struct {
	// SessionID is the unique session identifier (used for --session-id).
	SessionID string

	// WorkDir is the working directory for the process.
	WorkDir string

	// Prompt is the initial prompt to send to Claude.
	Prompt string

	// Model specifies which model to use (sonnet, opus, haiku).
	Model string

	// PermissionMode is the permission level (plan, default, etc).
	PermissionMode string

	// AllowedTools restricts which tools the agent can use.
	AllowedTools []string

	// SystemPrompt is appended to Claude's system prompt.
	SystemPrompt string

	// Resume indicates we're resuming an existing session.
	Resume bool

	// ForkSession forks from an existing session.
	ForkSession string

	// MaxBudgetUSD limits spending (only works with --print).
	MaxBudgetUSD float64
}

// CommandString returns the full command that would be executed (for logging).
func (o *SpawnOptions) CommandString() string {
	args := o.Args()
	// Quote args that contain spaces
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.Contains(arg, " ") || strings.Contains(arg, "\n") {
			// Truncate long prompts for readability
			if len(arg) > 100 {
				arg = arg[:97] + "..."
			}
			quoted[i] = `"` + arg + `"`
		} else {
			quoted[i] = arg
		}
	}
	return "claude " + strings.Join(quoted, " ")
}

// Args builds the command-line arguments for Claude Code.
func (o *SpawnOptions) Args() []string {
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	if o.SessionID != "" {
		args = append(args, "--session-id", o.SessionID)
	}

	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}

	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}

	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}

	if o.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", o.SystemPrompt)
	}

	if o.Resume {
		args = append(args, "--resume")
	}

	if o.ForkSession != "" {
		args = append(args, "--fork-session", o.ForkSession)
	}

	if o.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(o.MaxBudgetUSD, 'f', 2, 64))
	}

	// Prompt is sent via stdin when using stream-json input format
	// (not as a CLI argument)

	return args
}

// PlannerOptions returns options configured for a planning agent.
func PlannerOptions(sessionID, workDir, prompt string) *SpawnOptions {
	return &SpawnOptions{
		SessionID:      sessionID,
		WorkDir:        workDir,
		Prompt:         prompt,
		Model:          "sonnet",
		PermissionMode: "plan",
		AllowedTools:   []string{"Glob", "Grep", "Read", "Task", "WebFetch", "WebSearch"},
		SystemPrompt:   "You are a planning agent. Explore the codebase thoroughly and draft a detailed implementation plan. Do NOT modify any files.",
	}
}

// ExecutorOptions returns options configured for an execution agent.
func ExecutorOptions(sessionID, workDir, prompt string) *SpawnOptions {
	return &SpawnOptions{
		SessionID:      sessionID,
		WorkDir:        workDir,
		Prompt:         prompt,
		Model:          "sonnet",
		PermissionMode: "default",
		SystemPrompt:   "You are an execution agent. Follow the provided plan exactly. Report progress after each step.",
	}
}

// ReviewerOptions returns options configured for a code review agent.
func ReviewerOptions(sessionID, workDir, prompt string) *SpawnOptions {
	return &SpawnOptions{
		SessionID:      sessionID,
		WorkDir:        workDir,
		Prompt:         prompt,
		Model:          "sonnet",
		PermissionMode: "plan",
		AllowedTools:   []string{"Glob", "Grep", "Read", "Task"},
		SystemPrompt:   "You are a code review agent. Analyze changes for bugs, security issues, and style violations.",
	}
}

// ArchitectOptions returns options configured for an architect agent.
func ArchitectOptions(sessionID, workDir, prompt string) *SpawnOptions {
	return &SpawnOptions{
		SessionID:      sessionID,
		WorkDir:        workDir,
		Prompt:         prompt,
		Model:          "opus",
		PermissionMode: "plan",
		AllowedTools:   []string{"Glob", "Grep", "Read", "Task"},
		SystemPrompt:   "You are an architect agent. Break down tasks into independent subtasks. Output structured JSON with subtasks, merge_strategy, and integration_notes.",
	}
}
