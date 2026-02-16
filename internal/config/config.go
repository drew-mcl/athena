// Package config handles Athena configuration loading and validation.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// JobsConfig defines job execution settings.
type JobsConfig struct {
	// Quick job safety limits
	MaxFiles      int `yaml:"max_files"`
	MaxInsertions int `yaml:"max_insertions"`
	MaxDeletions  int `yaml:"max_deletions"`

	// Message truncation
	MaxCommitMessageLength int `yaml:"max_commit_message_length"`
	MaxLogTruncateLength   int `yaml:"max_log_truncate_length"`

	// Timeouts
	QuickJobTimeout time.Duration `yaml:"quick_job_timeout"`
}

// FeatureFlags controls experimental features.
type FeatureFlags struct {
	ClaudeTasks bool `yaml:"claude_tasks"` // Enable Claude Code tasks tab
}

// Config is the root configuration for Athena.
type Config struct {
	Repos        ReposConfig          `yaml:"repos"`
	Agents       AgentsConfig         `yaml:"agents"`
	Archetypes   map[string]Archetype `yaml:"archetypes"`
	Terminal     TerminalConfig       `yaml:"terminal"`
	Daemon       DaemonConfig         `yaml:"daemon"`
	Integrations IntegrationsConfig   `yaml:"integrations"`
	Gemini       GeminiConfig         `yaml:"gemini"`
	Jobs         JobsConfig           `yaml:"jobs"`
	UI           UIConfig             `yaml:"ui"`
	Features     FeatureFlags         `yaml:"features"`
}

// GeminiConfig defines Google Gemini integration settings.
type GeminiConfig struct {
	APIKey string `yaml:"api_key"`
	Model  string `yaml:"model"`
}

// ReposConfig defines repository discovery settings.
type ReposConfig struct {
	BaseDirs     []string      `yaml:"base_dirs"`
	WorktreeDir  string        `yaml:"worktree_dir"` // Dedicated directory for worktrees
	Exclude      []string      `yaml:"exclude"`
	Include      []string      `yaml:"include"`
	ScanInterval time.Duration `yaml:"scan_interval"`
}

// AgentsConfig defines default agent behavior.
type AgentsConfig struct {
	RestartPolicy     string        `yaml:"restart_policy"`
	MaxRestarts       int           `yaml:"max_restarts"`
	RestartBackoff    BackoffConfig `yaml:"restart_backoff"`
	Provider          string        `yaml:"provider"`
	Model             string        `yaml:"model"`
	Budget            BudgetConfig  `yaml:"budget"`
	ContextRetention  time.Duration `yaml:"context_retention"`
	MaxContextTokens  int           `yaml:"max_context_tokens"` // Max tokens for context block
	MaxRelevantFiles  int           `yaml:"max_relevant_files"` // Max relevant files to include
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout"`
	SkipPermissions   *bool         `yaml:"skip_permissions"` // Skip all permission checks (default: true)
}

// BackoffConfig defines exponential backoff parameters.
type BackoffConfig struct {
	Initial    time.Duration `yaml:"initial"`
	Max        time.Duration `yaml:"max"`
	Multiplier float64       `yaml:"multiplier"`
}

// BudgetConfig defines spending limits.
type BudgetConfig struct {
	MaxPerAgent   float64 `yaml:"max_per_agent"`
	MaxPerDay     float64 `yaml:"max_per_day"`
	WarnThreshold float64 `yaml:"warn_threshold"`
}

// Archetype defines a reusable agent configuration.
type Archetype struct {
	Description    string   `yaml:"description"`
	Prompt         string   `yaml:"prompt"`
	PermissionMode string   `yaml:"permission_mode"`
	AllowedTools   []string `yaml:"allowed_tools"`
	Provider       string   `yaml:"provider"`
	Model          string   `yaml:"model"`
}

// TerminalConfig defines terminal emulator integration.
type TerminalConfig struct {
	Provider     string `yaml:"provider"`
	SpawnCommand string `yaml:"spawn_command"`
	AutoAttach   bool   `yaml:"auto_attach"`
}

// DaemonConfig defines athenad settings.
type DaemonConfig struct {
	Socket    string        `yaml:"socket"`
	Database  string        `yaml:"database"`
	LogFile   string        `yaml:"log_file"`
	LogLevel  string        `yaml:"log_level"`
	SentryDSN string        `yaml:"sentry_dsn"`
	Metrics   MetricsConfig `yaml:"metrics"`
}

// MetricsConfig defines optional metrics endpoint.
type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// IntegrationsConfig defines external service connections.
type IntegrationsConfig struct {
	Linear     LinearConfig          `yaml:"linear"`
	GitHub     GitHubConfig          `yaml:"github"`
	Identities AgentIdentitiesConfig `yaml:"identities"`
}

// AgentIdentitiesConfig defines git identities for agent commits.
// This enables agents to commit as bot users (ata-codex, ata-clc) with
// the human user as co-author.
type AgentIdentitiesConfig struct {
	// Default identity used when no archetype-specific identity is configured.
	Default *AgentIdentity `yaml:"default"`

	// Archetypes maps archetype names to specific identities.
	// Example: executor -> ata-clc (Claude Code does hands-on work)
	Archetypes map[string]*AgentIdentity `yaml:"archetypes"`

	// CoAuthor configures the human co-author for agent commits.
	CoAuthor *CoAuthorConfig `yaml:"co_author"`
}

// AgentIdentity represents a git identity for an agent.
// Can optionally include GitHub App credentials for PR creation.
type AgentIdentity struct {
	// Name is the git author/committer name (e.g., "ata-codex").
	Name string `yaml:"name"`

	// Email is the git author/committer email (e.g., "ata-codex[bot]@users.noreply.github.com").
	Email string `yaml:"email"`

	// GitHubAppID is the GitHub App ID for API authentication.
	GitHubAppID string `yaml:"github_app_id"`

	// PrivateKeyPath is the path to the GitHub App private key (.pem file).
	PrivateKeyPath string `yaml:"private_key_path"`

	// InstallationID is the GitHub App installation ID for the target org/repos.
	InstallationID string `yaml:"installation_id"`
}

// CoAuthorConfig defines the human co-author for agent commits.
type CoAuthorConfig struct {
	// Enabled controls whether co-author trailer is added to commits.
	Enabled bool `yaml:"enabled"`

	// Name is the co-author's name.
	Name string `yaml:"name"`

	// Email is the co-author's email.
	Email string `yaml:"email"`
}

// HasGitHubApp returns true if this identity has GitHub App credentials configured.
func (i *AgentIdentity) HasGitHubApp() bool {
	return i != nil && i.GitHubAppID != "" && i.PrivateKeyPath != "" && i.InstallationID != ""
}

// CoAuthorLine returns the Git trailer for co-authorship.
func (c *CoAuthorConfig) CoAuthorLine() string {
	if c == nil || !c.Enabled || c.Name == "" || c.Email == "" {
		return ""
	}
	return "Co-authored-by: " + c.Name + " <" + c.Email + ">"
}

// LinearConfig defines Linear integration settings.
type LinearConfig struct {
	Enabled         bool     `yaml:"enabled"`
	WebhookSecret   string   `yaml:"webhook_secret"`
	APIKey          string   `yaml:"api_key"`
	AutoPlan        bool     `yaml:"auto_plan"`
	AutoPlanLabels  []string `yaml:"auto_plan_labels"`
	PostPlanComment bool     `yaml:"post_plan_comment"`
}

// GitHubConfig defines GitHub integration settings.
type GitHubConfig struct {
	Enabled     bool   `yaml:"enabled"`
	AutoPR      bool   `yaml:"auto_pr"`
	PRTemplate  string `yaml:"pr_template"`
	AutoMerge   bool   `yaml:"auto_merge"`   // Auto-merge PRs when CI green + no conflicts (default: true)
	MergeMethod string `yaml:"merge_method"` // "rebase", "squash", or "merge" (default: "rebase")
}

// WorkflowMode controls automation level for agent spawning
type WorkflowMode string

const (
	WorkflowModeAutomatic WorkflowMode = "automatic" // Auto-plan, auto-approve, auto-execute
	WorkflowModeApprove   WorkflowMode = "approve"   // Auto-plan, manual approval, then execute
	WorkflowModeManual    WorkflowMode = "manual"    // Everything requires explicit user input
)

// UIConfig defines TUI appearance.
type UIConfig struct {
	Theme           string            `yaml:"theme"`
	Colors          map[string]string `yaml:"colors"`
	ShowActivity    bool              `yaml:"show_activity"`
	ActivityHeight  int               `yaml:"activity_height"`
	RefreshInterval time.Duration     `yaml:"refresh_interval"`
	WorkflowMode    WorkflowMode      `yaml:"workflow_mode"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	homeDir := resolveHomeDir()

	return &Config{
		Repos: ReposConfig{
			BaseDirs:     []string{filepath.Join(homeDir, "repos")},
			WorktreeDir:  filepath.Join(homeDir, "repos/worktrees"),
			Exclude:      []string{"**/node_modules", "**/vendor", "**/.git", "**/target", "**/worktrees"},
			ScanInterval: 5 * time.Minute,
		},
		Agents: AgentsConfig{
			RestartPolicy:     "on-failure",
			MaxRestarts:       3,
			RestartBackoff:    BackoffConfig{Initial: 5 * time.Second, Max: 5 * time.Minute, Multiplier: 2.0},
			Provider:          "claude",
			Model:             "opus",
			Budget:            BudgetConfig{MaxPerAgent: 5.0, MaxPerDay: 50.0, WarnThreshold: 0.8},
			ContextRetention:  7 * 24 * time.Hour,
			MaxContextTokens:  100000,
			MaxRelevantFiles:  50,
			HeartbeatInterval: 30 * time.Second,
			HeartbeatTimeout:  2 * time.Minute,
			SkipPermissions:   boolPtr(true),
		},
		Archetypes: map[string]Archetype{
			"planner": {
				Description: "Planning agent that explores codebases and creates implementation plans",
				Prompt: `You are a planning agent. Create a detailed implementation plan. Do NOT modify any files.

## Using the Context Block
Your prompt is prepended with context sections:
- **Project State**: Architecture decisions, conventions, constraints - follow these
- **Project Structure**: Directory layout - understand where code lives
- **Relevant Files**: START HERE - these files match your task based on code analysis
- **Workflow Context**: Decisions/findings from previous agents - don't repeat their work

## Your Workflow
1. Read the Relevant Files first - they're pre-selected for your task
2. Use the Project Structure to navigate - explore related directories
3. Check if previous agents already explored this area (see Workflow Context)
4. Create a detailed plan with specific files and implementation steps

## Recording Your Work
Use markers in your output to help future agents:
- [[DECISION: <what you decided and why>]]
- [[FINDING: <important discovery about the code>]]
- [[TRIED: <approach you attempted and the result>]]
- [[QUESTION: <unresolved question for human review>]]`,
				PermissionMode: "plan",
				AllowedTools:   []string{"Glob", "Grep", "Read", "Task", "WebFetch", "WebSearch"},
				Model:          "opus",
			},
			"executor": {
				Description: "Execution agent that implements plans and commits changes",
				Prompt: `You are an execution agent. Follow the provided plan exactly.

## Using the Context Block
- **Project State**: Conventions to follow (naming, patterns, architecture)
- **Relevant Files**: Files identified for this task - read them first
- **Workflow Context**: Prior decisions - build on the planner's work, don't redo research

## Your Workflow
1. Read the plan carefully - understand all steps before starting
2. Read Relevant Files to understand existing patterns
3. Implement changes step by step, following project conventions
4. Test your changes before committing

## Recording Your Work
Record important information for future agents:
- [[DECISION: <implementation choice you made>]]
- [[FINDING: <discovered constraint or pattern>]]
- [[TRIED: <approach and outcome>]]

## CRITICAL: Completion Requirements

When your coding work is complete, you MUST perform ALL of these steps in order:

### 1. Commit All Changes
- Use conventional commit format (feat:, fix:, refactor:, etc.)
- Write a clear subject line and explanatory body
- Include ticket ID in commit message if applicable
- Never leave uncommitted changes in the worktree

### 2. Push to Remote
- Push your commits to the remote branch
- Ensure the push succeeds before proceeding

### 3. Create Pull Request
- ALWAYS use gh pr create or the /commit-push-pr skill
- Include the ticket ID in the PR title if applicable
- Provide a clear summary of changes in the PR body
- Verify the PR was created successfully
- Creating the PR will automatically mark this feature as complete

IMPORTANT: All three steps are REQUIRED. Do not skip any step. If any step fails, resolve the issue before proceeding.`,
				PermissionMode: "default",
				Model:          "sonnet",
			},
			"reviewer": {
				Description: "Code review agent that analyzes changes",
				Prompt: `You are a code review agent. Analyze changes for bugs, security issues, and style violations.

## Using the Context Block
- **Project State**: Conventions and constraints to check against
- **Relevant Files**: Files related to the changes being reviewed
- **Workflow Context**: What the implementer tried and decided

## Review Checklist
1. Read the Relevant Files to understand the codebase patterns
2. Check changes against Project State conventions
3. Look for bugs, security issues, performance problems
4. Verify tests exist and are meaningful

## Recording Your Findings
- [[FINDING: <issue found with severity and location>]]
- [[DECISION: <approval/rejection with rationale>]]`,
				PermissionMode: "plan",
				AllowedTools:   []string{"Glob", "Grep", "Read", "Task"},
				Model:          "sonnet",
			},
			"brainstorm": {
				Description: "Interactive brainstorming session for collaborative ideation",
				Prompt: `You are a collaborative brainstorming partner. Help the user explore ideas, understand requirements, and develop an implementation approach.

## Your Role
- Engage in natural conversation to understand what the user wants to build
- Ask clarifying questions to uncover requirements and edge cases
- Explore the codebase together to understand existing patterns
- Suggest approaches and discuss trade-offs
- Help refine ideas until they're ready for implementation

## When Ready to Plan
When you and the user have a clear understanding of the feature and approach:
1. Summarize the agreed-upon design decisions
2. Use EnterPlanMode to create a formal implementation plan
3. The plan will be saved and can be reviewed/executed in Athena

## Guidelines
- Be conversational and exploratory, not prescriptive
- Ask questions before assuming requirements
- Reference existing code patterns when relevant
- Document decisions as you go using [[DECISION: ...]] markers
- Keep the user engaged and in control of direction`,
				PermissionMode: "plan",
				AllowedTools:   []string{"Glob", "Grep", "Read", "Task", "WebFetch", "WebSearch"},
				Model:          "opus",
			},
			"orchestrator": {
				Description: "Goal orchestrator that breaks down goals into features and coordinates implementation",
				Prompt: `You are a goal orchestrator. Your role is to analyze high-level goals, break them into features, and coordinate their implementation.

## Your Workflow

1. **Analyze the Goal**
   - Read the goal description carefully
   - Explore the codebase to understand current architecture
   - Identify what needs to change to achieve the goal

2. **Break Down into Features**
   - Decompose the goal into discrete, implementable features
   - For each feature, define clear scope and acceptance criteria
   - Create Feature work items under the goal using TaskCreate

3. **Evaluate Complexity**
   - **Work solo if:**
     - Goal has < 5 features
     - Features are sequential (dependencies between them)
     - Changes are localized to a few files
     - Estimated work is < 10 tasks total

   - **Create a team if:**
     - Goal has 5+ independent features
     - Multiple areas of codebase need changes
     - Work can be parallelized (frontend + backend, multiple services)
     - Estimated work is > 10 tasks or spans multiple days

4. **Solo Approach**
   - Work through features sequentially
   - For each feature: explore, plan, implement, commit
   - Mark features completed as you finish them

5. **Team Approach**
   - Use TeamCreate to create a coordinated team
   - Spawn teammates for each feature using the Task tool with subagent_type="general-purpose"
   - Assign features to teammates using TaskUpdate
   - Coordinate their work and integrate results
   - Handle blockers and conflicts as they arise

## Recording Your Work
- [[DECISION: <architectural choice or approach>]]
- [[FINDING: <important discovery about the codebase>]]
- [[FEATURE: <feature breakdown with scope>]]

## Important Notes
- Always create Feature work items - don't work directly on the goal
- Features should be independently testable and reviewable
- When spawning teammates, give them clear, focused feature scopes
- Integrate work frequently to avoid large merge conflicts`,
				PermissionMode: "default",
				AllowedTools:   []string{"Bash", "Glob", "Grep", "Read", "Edit", "Write", "Task", "TeamCreate", "SendMessage", "WebFetch", "WebSearch"},
				Model:          "opus",
			},
			"reconciler": {
				Description:    "Maintenance agent for branch cleanup, queue reconciliation, and PR management",
				Prompt:         reconcilerPrompt,
				PermissionMode: "default",
				AllowedTools:   []string{"Bash", "Read", "Grep", "Glob", "Edit", "Write", "WebFetch"},
				Model:          "sonnet",
			},
			"mapper": {
				Description:    "Codebase mapper that explores and documents project structure",
				Prompt:         mapperPrompt,
				PermissionMode: "default",
				AllowedTools:   []string{"Bash", "Read", "Grep", "Glob", "Edit", "Write"},
				Model:          "sonnet",
			},
		},
		Terminal: TerminalConfig{
			Provider:   "ghostty",
			AutoAttach: false,
		},
		Integrations: IntegrationsConfig{
			GitHub: GitHubConfig{
				AutoMerge:   true,
				MergeMethod: "rebase",
			},
		},
		Daemon: DaemonConfig{
			Socket:   "/tmp/athena.sock",
			Database: filepath.Join(homeDir, ".local/share/athena/athena.db"),
			LogFile:  filepath.Join(homeDir, ".local/share/athena/athena.log"),
			LogLevel: "info",
			Metrics:  MetricsConfig{Enabled: false, Port: 9090},
		},
		Gemini: GeminiConfig{
			Model: "gemini-2.0-flash-exp",
		},
		Jobs: JobsConfig{
			MaxFiles:               50,
			MaxInsertions:          1000,
			MaxDeletions:           1000,
			MaxCommitMessageLength: 72,
			MaxLogTruncateLength:   50,
			QuickJobTimeout:        5 * time.Minute,
		},
		UI: UIConfig{
			Theme:           "tokyo-night",
			ShowActivity:    true,
			ActivityHeight:  5,
			RefreshInterval: time.Second,
			WorkflowMode:    WorkflowModeApprove, // Default to approve - sensible middle ground
		},
		Features: FeatureFlags{
			ClaudeTasks: false, // Disabled by default; enable via ATHENA_CLAUDE_TASKS=true
		},
	}
}

// GetJobLimits returns configured job safety limits.
func (c *Config) GetJobLimits() (maxFiles, maxInsertions, maxDeletions int) {
	return c.Jobs.MaxFiles, c.Jobs.MaxInsertions, c.Jobs.MaxDeletions
}

// GetTruncateLengths returns configured truncation lengths.
func (c *Config) GetTruncateLengths() (commitMsg, logMsg int) {
	return c.Jobs.MaxCommitMessageLength, c.Jobs.MaxLogTruncateLength
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() string {
	if p := os.Getenv("ATHENA_CONFIG"); p != "" {
		return p
	}
	homeDir := resolveHomeDir()
	return filepath.Join(homeDir, ".config/athena/config.yaml")
}

func boolPtr(b bool) *bool {
	return &b
}

// ShouldSkipPermissions returns whether agents should skip permission checks.
// Defaults to true when not explicitly configured.
func (c *AgentsConfig) ShouldSkipPermissions() bool {
	if c.SkipPermissions == nil {
		return true
	}
	return *c.SkipPermissions
}

func resolveHomeDir() string {
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		return homeDir
	}
	if homeDir := os.Getenv("HOME"); homeDir != "" {
		return homeDir
	}
	return "."
}

// Load loads the configuration from the default path.
func Load() (*Config, error) {
	configPath := DefaultConfigPath()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return default config if file doesn't exist
		cfg := DefaultConfig()
		cfg.applyEnvOverrides()
		return cfg, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", configPath, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", configPath, err)
	}

	cfg.expandEnvVars()
	cfg.applyEnvOverrides()
	return cfg, nil
}

func (c *Config) expandEnvVars() {
	c.Integrations.Linear.WebhookSecret = os.ExpandEnv(c.Integrations.Linear.WebhookSecret)
	c.Integrations.Linear.APIKey = os.ExpandEnv(c.Integrations.Linear.APIKey)
	c.Daemon.SentryDSN = os.ExpandEnv(c.Daemon.SentryDSN)
	c.Gemini.APIKey = os.ExpandEnv(c.Gemini.APIKey)

	// Expand env vars in identity config
	if c.Integrations.Identities.Default != nil {
		c.Integrations.Identities.Default.expandEnvVars()
	}
	for _, identity := range c.Integrations.Identities.Archetypes {
		if identity != nil {
			identity.expandEnvVars()
		}
	}
}

func (i *AgentIdentity) expandEnvVars() {
	i.GitHubAppID = os.ExpandEnv(i.GitHubAppID)
	i.PrivateKeyPath = os.ExpandEnv(i.PrivateKeyPath)
	i.InstallationID = os.ExpandEnv(i.InstallationID)

	// Expand ~ in private key path
	if strings.HasPrefix(i.PrivateKeyPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			i.PrivateKeyPath = filepath.Join(home, i.PrivateKeyPath[2:])
		}
	}
}

// applyEnvOverrides applies environment variable overrides to config.
// These override both default values and config file values.
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("ATHENA_CLAUDE_TASKS"); v == "true" || v == "1" {
		c.Features.ClaudeTasks = true
	}
}

// CycleWorkflowMode cycles through workflow modes: automatic → approve → manual → automatic
func (m WorkflowMode) CycleWorkflowMode() WorkflowMode {
	switch m {
	case WorkflowModeAutomatic:
		return WorkflowModeApprove
	case WorkflowModeApprove:
		return WorkflowModeManual
	case WorkflowModeManual:
		return WorkflowModeAutomatic
	default:
		return WorkflowModeApprove
	}
}
