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
	Model             string        `yaml:"model"`
	Budget            BudgetConfig  `yaml:"budget"`
	ContextRetention  time.Duration `yaml:"context_retention"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout"`
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
	Enabled    bool   `yaml:"enabled"`
	AutoPR     bool   `yaml:"auto_pr"`
	PRTemplate string `yaml:"pr_template"`
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
	homeDir, _ := os.UserHomeDir()

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
			Model:             "sonnet",
			Budget:            BudgetConfig{MaxPerAgent: 5.0, MaxPerDay: 50.0, WarnThreshold: 0.8},
			ContextRetention:  7 * 24 * time.Hour,
			HeartbeatInterval: 30 * time.Second,
			HeartbeatTimeout:  2 * time.Minute,
		},
		Archetypes: map[string]Archetype{},
		Terminal: TerminalConfig{
			Provider:   "ghostty",
			AutoAttach: false,
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
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config/athena/config.yaml")
}

// Load loads the configuration from the default path.
func Load() (*Config, error) {
	configPath := DefaultConfigPath()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return default config if file doesn't exist
		return DefaultConfig(), nil
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
