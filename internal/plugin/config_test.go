package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissingReturnsEmptyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig() returned nil config")
	}
	if cfg.Enabled == nil {
		t.Fatal("expected Enabled map to be initialized")
	}
	if cfg.IsEnabled("github") {
		t.Fatal("expected github to be disabled by default")
	}
}

func TestSaveConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	in := &Config{
		Enabled: map[string]bool{
			"github": true,
			"linear": true,
			"jira":   false,
		},
	}

	if err := SaveConfig(in); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	path := filepath.Join(home, ".config", "athena", "plugins.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file at %s: %v", path, err)
	}

	out, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if !out.IsEnabled("github") {
		t.Fatal("expected github enabled after round-trip")
	}
	if !out.IsEnabled("linear") {
		t.Fatal("expected linear enabled after round-trip")
	}
	if out.IsEnabled("jira") {
		t.Fatal("expected jira disabled after round-trip")
	}
}

func TestApplyConfig(t *testing.T) {
	r := NewRegistry()
	gh := NewBasePlugin("github", CategoryVCS)
	jira := NewBasePlugin("jira", CategoryPM)
	r.Register(gh)
	r.Register(jira)

	r.ApplyConfig(&Config{
		Enabled: map[string]bool{
			"github": true,
			"jira":   false,
		},
	})

	if !gh.Enabled() {
		t.Fatal("expected github plugin enabled")
	}
	if jira.Enabled() {
		t.Fatal("expected jira plugin disabled")
	}
}

func TestRefreshRegistryFromDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SaveConfig(&Config{
		Enabled: map[string]bool{
			"github": true,
			"jira":   false,
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	r := NewRegistry()
	gh := NewBasePlugin("github", CategoryVCS)
	jira := NewBasePlugin("jira", CategoryPM)
	r.Register(gh)
	r.Register(jira)

	if err := RefreshRegistryFromDisk(r); err != nil {
		t.Fatalf("RefreshRegistryFromDisk() error = %v", err)
	}
	if !gh.Enabled() {
		t.Fatal("expected github plugin enabled from disk config")
	}
	if jira.Enabled() {
		t.Fatal("expected jira plugin disabled from disk config")
	}
}
