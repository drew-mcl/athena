package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnableCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := Enable(dir); err != nil {
		t.Fatal(err)
	}

	path := SettingsPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	var hooks map[string][]hookMatcher
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatal(err)
	}

	// Check all three events are present
	for _, event := range []string{"SessionStart", "Stop", "SessionEnd"} {
		matchers, ok := hooks[event]
		if !ok {
			t.Errorf("missing event %s", event)
			continue
		}
		found := false
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if isAthenaHook(h.Command) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("no athena hook found for %s", event)
		}
	}
}

func TestEnablePreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".claude")
	os.MkdirAll(settingsDir, 0o755)

	// Write existing settings with entire hooks
	existing := `{
  "hooks": {
    "SessionStart": [
      {"matcher": "", "hooks": [{"type": "command", "command": "entire hooks claude-code session-start"}]}
    ],
    "PreToolUse": [
      {"matcher": "Task", "hooks": [{"type": "command", "command": "entire hooks claude-code pre-task"}]}
    ]
  },
  "permissions": {
    "deny": ["Read(./.entire/metadata/**)"]
  }
}`
	os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(existing), 0o644)

	if err := Enable(dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(SettingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	// Check permissions preserved
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions key was lost")
	}

	var hooks map[string][]hookMatcher
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatal(err)
	}

	// Check entire hooks still present
	sessionStart := hooks["SessionStart"]
	if len(sessionStart) != 2 {
		t.Errorf("expected 2 SessionStart matchers, got %d", len(sessionStart))
	}

	// Check entire hook preserved
	foundEntire := false
	for _, m := range sessionStart {
		for _, h := range m.Hooks {
			if h.Command == "entire hooks claude-code session-start" {
				foundEntire = true
			}
		}
	}
	if !foundEntire {
		t.Error("entire hook was not preserved")
	}

	// Check PreToolUse preserved (not touched by athena)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse hooks were lost")
	}
}

func TestEnableIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	if err := Enable(dir); err != nil {
		t.Fatal(err)
	}

	first, _ := os.ReadFile(SettingsPath(dir))

	if err := Enable(dir); err != nil {
		t.Fatal(err)
	}

	second, _ := os.ReadFile(SettingsPath(dir))

	if string(first) != string(second) {
		t.Error("enable was not idempotent")
		t.Logf("first:\n%s", first)
		t.Logf("second:\n%s", second)
	}
}

func TestDisableRemovesOnlyAthena(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".claude")
	os.MkdirAll(settingsDir, 0o755)

	// Write settings with both entire and athena hooks
	existing := `{
  "hooks": {
    "SessionStart": [
      {"matcher": "", "hooks": [{"type": "command", "command": "entire hooks claude-code session-start"}]},
      {"matcher": "", "hooks": [{"type": "command", "command": "ath hooks session-start"}]}
    ],
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "entire hooks claude-code stop"}]},
      {"matcher": "", "hooks": [{"type": "command", "command": "ath hooks stop"}]}
    ],
    "SessionEnd": [
      {"matcher": "", "hooks": [{"type": "command", "command": "ath hooks session-end"}]}
    ]
  },
  "permissions": {
    "deny": ["Read(./.entire/metadata/**)"]
  }
}`
	os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(existing), 0o644)

	if err := Disable(dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(SettingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	var hooks map[string][]hookMatcher
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatal(err)
	}

	// SessionStart should still have entire hook
	sessionStart := hooks["SessionStart"]
	if len(sessionStart) != 1 {
		t.Errorf("expected 1 SessionStart matcher, got %d", len(sessionStart))
	}
	if sessionStart[0].Hooks[0].Command != "entire hooks claude-code session-start" {
		t.Error("wrong hook preserved")
	}

	// Stop should still have entire hook
	stop := hooks["Stop"]
	if len(stop) != 1 {
		t.Errorf("expected 1 Stop matcher, got %d", len(stop))
	}

	// SessionEnd had only athena hook, should be removed entirely
	if _, ok := hooks["SessionEnd"]; ok {
		t.Error("SessionEnd should have been removed (only had athena hook)")
	}

	// Permissions preserved
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions key was lost")
	}
}

func TestIsEnabled(t *testing.T) {
	dir := t.TempDir()

	if IsEnabled(dir) {
		t.Error("should not be enabled before install")
	}

	Enable(dir)
	if !IsEnabled(dir) {
		t.Error("should be enabled after install")
	}

	Disable(dir)
	if IsEnabled(dir) {
		t.Error("should not be enabled after disable")
	}
}

func TestDisableNoFile(t *testing.T) {
	dir := t.TempDir()

	// Should not error on missing file
	if err := Disable(dir); err != nil {
		t.Fatal(err)
	}
}
