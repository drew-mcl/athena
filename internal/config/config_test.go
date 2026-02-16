package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"agents provider", cfg.Agents.Provider, "claude"},
		{"agents model", cfg.Agents.Model, "opus"},
		{"agents restart policy", cfg.Agents.RestartPolicy, "on-failure"},
		{"agents max restarts", cfg.Agents.MaxRestarts, 3},
		{"budget max per agent", cfg.Agents.Budget.MaxPerAgent, 5.0},
		{"budget max per day", cfg.Agents.Budget.MaxPerDay, 50.0},
		{"budget warn threshold", cfg.Agents.Budget.WarnThreshold, 0.8},
		{"backoff initial", cfg.Agents.RestartBackoff.Initial, 5 * time.Second},
		{"backoff max", cfg.Agents.RestartBackoff.Max, 5 * time.Minute},
		{"backoff multiplier", cfg.Agents.RestartBackoff.Multiplier, 2.0},
		{"terminal provider", cfg.Terminal.Provider, "ghostty"},
		{"daemon socket", cfg.Daemon.Socket, "/tmp/athena.sock"},
		{"daemon log level", cfg.Daemon.LogLevel, "info"},
		{"metrics enabled", cfg.Daemon.Metrics.Enabled, false},
		{"metrics port", cfg.Daemon.Metrics.Port, 9090},
		{"ui theme", cfg.UI.Theme, "tokyo-night"},
		{"ui workflow mode", cfg.UI.WorkflowMode, WorkflowModeApprove},
		{"ui show activity", cfg.UI.ShowActivity, true},
		{"ui activity height", cfg.UI.ActivityHeight, 5},
		{"gemini model", cfg.Gemini.Model, "gemini-2.0-flash-exp"},
		{"jobs max files", cfg.Jobs.MaxFiles, 50},
		{"jobs max insertions", cfg.Jobs.MaxInsertions, 1000},
		{"jobs max deletions", cfg.Jobs.MaxDeletions, 1000},
		{"jobs commit msg len", cfg.Jobs.MaxCommitMessageLength, 72},
		{"jobs quick timeout", cfg.Jobs.QuickJobTimeout, 5 * time.Minute},
		{"features claude tasks", cfg.Features.ClaudeTasks, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}

	// Check archetypes exist
	archetypes := []string{"planner", "executor", "reviewer", "brainstorm"}
	for _, name := range archetypes {
		if _, ok := cfg.Archetypes[name]; !ok {
			t.Errorf("missing archetype: %s", name)
		}
	}

	// Check planner archetype details
	planner := cfg.Archetypes["planner"]
	if planner.PermissionMode != "plan" {
		t.Errorf("planner permission mode: got %s, want plan", planner.PermissionMode)
	}
	if planner.Model != "opus" {
		t.Errorf("planner model: got %s, want opus", planner.Model)
	}

	// Check executor archetype details
	executor := cfg.Archetypes["executor"]
	if executor.PermissionMode != "default" {
		t.Errorf("executor permission mode: got %s, want default", executor.PermissionMode)
	}
	if executor.Model != "sonnet" {
		t.Errorf("executor model: got %s, want sonnet", executor.Model)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	// Clear env var for clean test
	orig := os.Getenv("ATHENA_CONFIG")
	os.Unsetenv("ATHENA_CONFIG")
	defer func() {
		if orig != "" {
			os.Setenv("ATHENA_CONFIG", orig)
		}
	}()

	path := DefaultConfigPath()
	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".config/athena/config.yaml")
	if path != expected {
		t.Errorf("got %s, want %s", path, expected)
	}
}

func TestDefaultConfigPathFromEnv(t *testing.T) {
	os.Setenv("ATHENA_CONFIG", "/custom/path/config.yaml")
	defer os.Unsetenv("ATHENA_CONFIG")

	path := DefaultConfigPath()
	if path != "/custom/path/config.yaml" {
		t.Errorf("got %s, want /custom/path/config.yaml", path)
	}
}

func TestLoadReturnsDefaultsWhenNoFile(t *testing.T) {
	// Point to a non-existent file
	os.Setenv("ATHENA_CONFIG", "/tmp/athena-test-nonexistent-config.yaml")
	defer os.Unsetenv("ATHENA_CONFIG")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Agents.Provider != "claude" {
		t.Errorf("provider: got %s, want claude", cfg.Agents.Provider)
	}
}

func TestLoadParsesYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	yaml := `
repos:
  scan_interval: 10m
agents:
  provider: gemini
  model: flash
daemon:
  log_level: debug
ui:
  theme: catppuccin
  workflow_mode: automatic
`
	os.WriteFile(configPath, []byte(yaml), 0644)
	os.Setenv("ATHENA_CONFIG", configPath)
	defer os.Unsetenv("ATHENA_CONFIG")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Agents.Provider != "gemini" {
		t.Errorf("provider: got %s, want gemini", cfg.Agents.Provider)
	}
	if cfg.Agents.Model != "flash" {
		t.Errorf("model: got %s, want flash", cfg.Agents.Model)
	}
	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("log level: got %s, want debug", cfg.Daemon.LogLevel)
	}
	if cfg.UI.Theme != "catppuccin" {
		t.Errorf("theme: got %s, want catppuccin", cfg.UI.Theme)
	}
	if cfg.UI.WorkflowMode != WorkflowModeAutomatic {
		t.Errorf("workflow mode: got %s, want automatic", cfg.UI.WorkflowMode)
	}
	if cfg.Repos.ScanInterval != 10*time.Minute {
		t.Errorf("scan interval: got %v, want 10m", cfg.Repos.ScanInterval)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	os.WriteFile(configPath, []byte("{{invalid yaml"), 0644)
	os.Setenv("ATHENA_CONFIG", configPath)
	defer os.Unsetenv("ATHENA_CONFIG")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestGetJobLimits(t *testing.T) {
	cfg := DefaultConfig()
	maxFiles, maxInsertions, maxDeletions := cfg.GetJobLimits()

	if maxFiles != 50 {
		t.Errorf("maxFiles: got %d, want 50", maxFiles)
	}
	if maxInsertions != 1000 {
		t.Errorf("maxInsertions: got %d, want 1000", maxInsertions)
	}
	if maxDeletions != 1000 {
		t.Errorf("maxDeletions: got %d, want 1000", maxDeletions)
	}
}

func TestGetTruncateLengths(t *testing.T) {
	cfg := DefaultConfig()
	commitMsg, logMsg := cfg.GetTruncateLengths()

	if commitMsg != 72 {
		t.Errorf("commitMsg: got %d, want 72", commitMsg)
	}
	if logMsg != 50 {
		t.Errorf("logMsg: got %d, want 50", logMsg)
	}
}

func TestCycleWorkflowMode(t *testing.T) {
	tests := []struct {
		input WorkflowMode
		want  WorkflowMode
	}{
		{WorkflowModeAutomatic, WorkflowModeApprove},
		{WorkflowModeApprove, WorkflowModeManual},
		{WorkflowModeManual, WorkflowModeAutomatic},
		{WorkflowMode("unknown"), WorkflowModeApprove},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got := tt.input.CycleWorkflowMode()
			if got != tt.want {
				t.Errorf("CycleWorkflowMode(%s) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestAgentIdentityHasGitHubApp(t *testing.T) {
	tests := []struct {
		name     string
		identity *AgentIdentity
		want     bool
	}{
		{"nil identity", nil, false},
		{"empty identity", &AgentIdentity{}, false},
		{"partial - missing private key", &AgentIdentity{GitHubAppID: "123", InstallationID: "456"}, false},
		{"partial - missing installation", &AgentIdentity{GitHubAppID: "123", PrivateKeyPath: "/key.pem"}, false},
		{"complete", &AgentIdentity{GitHubAppID: "123", PrivateKeyPath: "/key.pem", InstallationID: "456"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.HasGitHubApp()
			if got != tt.want {
				t.Errorf("HasGitHubApp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoAuthorLine(t *testing.T) {
	tests := []struct {
		name   string
		config *CoAuthorConfig
		want   string
	}{
		{"nil config", nil, ""},
		{"disabled", &CoAuthorConfig{Enabled: false, Name: "Alice", Email: "alice@test.com"}, ""},
		{"missing name", &CoAuthorConfig{Enabled: true, Email: "alice@test.com"}, ""},
		{"missing email", &CoAuthorConfig{Enabled: true, Name: "Alice"}, ""},
		{"complete", &CoAuthorConfig{Enabled: true, Name: "Alice", Email: "alice@test.com"}, "Co-authored-by: Alice <alice@test.com>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.CoAuthorLine()
			if got != tt.want {
				t.Errorf("CoAuthorLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Features.ClaudeTasks {
		t.Fatal("ClaudeTasks should default to false")
	}

	os.Setenv("ATHENA_CLAUDE_TASKS", "true")
	defer os.Unsetenv("ATHENA_CLAUDE_TASKS")

	cfg.applyEnvOverrides()
	if !cfg.Features.ClaudeTasks {
		t.Error("ClaudeTasks should be true after env override")
	}
}

func TestExpandEnvVars(t *testing.T) {
	os.Setenv("TEST_LINEAR_KEY", "lin_test_key")
	os.Setenv("TEST_SENTRY_DSN", "https://sentry.io/test")
	defer func() {
		os.Unsetenv("TEST_LINEAR_KEY")
		os.Unsetenv("TEST_SENTRY_DSN")
	}()

	cfg := DefaultConfig()
	cfg.Integrations.Linear.APIKey = "$TEST_LINEAR_KEY"
	cfg.Daemon.SentryDSN = "$TEST_SENTRY_DSN"

	cfg.expandEnvVars()

	if cfg.Integrations.Linear.APIKey != "lin_test_key" {
		t.Errorf("Linear API key: got %s, want lin_test_key", cfg.Integrations.Linear.APIKey)
	}
	if cfg.Daemon.SentryDSN != "https://sentry.io/test" {
		t.Errorf("Sentry DSN: got %s, want https://sentry.io/test", cfg.Daemon.SentryDSN)
	}
}
