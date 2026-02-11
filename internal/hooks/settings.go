// Package hooks manages Claude Code lifecycle hooks in .claude/settings.json.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookEvents are the Claude Code events Athena hooks into.
var hookEvents = []string{"SessionStart", "Stop", "SessionEnd"}

// athenaCommand returns the ath hooks command for an event.
func athenaCommand(event string) string {
	switch event {
	case "SessionStart":
		return "ath hooks session-start"
	case "Stop":
		return "ath hooks stop"
	case "SessionEnd":
		return "ath hooks session-end"
	default:
		return ""
	}
}

// hookEntry represents a single hook entry in the settings.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookMatcher groups hooks under a matcher pattern.
type hookMatcher struct {
	Matcher string      `json:"matcher"`
	Hooks   []hookEntry `json:"hooks"`
}

// SettingsPath returns the path to .claude/settings.json in the given project root.
func SettingsPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "settings.json")
}

// Enable adds Athena hooks to the settings file, preserving existing hooks.
// Creates the file and directory if they don't exist.
func Enable(projectRoot string) error {
	path := SettingsPath(projectRoot)

	settings, err := loadSettings(path)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	hooks, err := getHooksMap(settings)
	if err != nil {
		return fmt.Errorf("parse hooks: %w", err)
	}

	for _, event := range hookEvents {
		cmd := athenaCommand(event)
		if cmd == "" {
			continue
		}
		addAthenaHook(hooks, event, cmd)
	}

	// Write hooks back
	hooksJSON, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}
	settings["hooks"] = json.RawMessage(hooksJSON)

	return saveSettings(path, settings)
}

// Disable removes Athena hooks from the settings file, preserving everything else.
func Disable(projectRoot string) error {
	path := SettingsPath(projectRoot)

	settings, err := loadSettings(path)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	hooks, err := getHooksMap(settings)
	if err != nil {
		return fmt.Errorf("parse hooks: %w", err)
	}

	for _, event := range hookEvents {
		removeAthenaHook(hooks, event)
	}

	// Write hooks back
	hooksJSON, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}
	settings["hooks"] = json.RawMessage(hooksJSON)

	return saveSettings(path, settings)
}

// IsEnabled checks if Athena hooks are currently installed.
func IsEnabled(projectRoot string) bool {
	path := SettingsPath(projectRoot)

	settings, err := loadSettings(path)
	if err != nil {
		return false
	}

	hooks, err := getHooksMap(settings)
	if err != nil {
		return false
	}

	// Check if any event has an Athena hook
	for _, event := range hookEvents {
		matchers, ok := hooks[event]
		if !ok {
			continue
		}
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if isAthenaHook(h.Command) {
					return true
				}
			}
		}
	}
	return false
}

// loadSettings reads and parses the settings file, returning a map of raw JSON fields.
func loadSettings(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]json.RawMessage), nil
		}
		return nil, err
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return settings, nil
}

// saveSettings writes the settings map back to disk with indentation.
func saveSettings(path string, settings map[string]json.RawMessage) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o644)
}

// getHooksMap extracts the hooks map from settings, keyed by event name.
func getHooksMap(settings map[string]json.RawMessage) (map[string][]hookMatcher, error) {
	hooks := make(map[string][]hookMatcher)

	raw, ok := settings["hooks"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return hooks, nil
	}

	if err := json.Unmarshal(raw, &hooks); err != nil {
		return nil, fmt.Errorf("unmarshal hooks: %w", err)
	}
	return hooks, nil
}

// addAthenaHook adds an Athena hook to the event if not already present.
func addAthenaHook(hooks map[string][]hookMatcher, event, command string) {
	matchers := hooks[event]

	// Check if already present
	for _, m := range matchers {
		for _, h := range m.Hooks {
			if isAthenaHook(h.Command) {
				return // Already installed
			}
		}
	}

	// Append new matcher entry
	hooks[event] = append(matchers, hookMatcher{
		Matcher: "",
		Hooks: []hookEntry{
			{Type: "command", Command: command},
		},
	})
}

// removeAthenaHook removes Athena hooks from an event, preserving others.
func removeAthenaHook(hooks map[string][]hookMatcher, event string) {
	matchers, ok := hooks[event]
	if !ok {
		return
	}

	var kept []hookMatcher
	for _, m := range matchers {
		var keptHooks []hookEntry
		for _, h := range m.Hooks {
			if !isAthenaHook(h.Command) {
				keptHooks = append(keptHooks, h)
			}
		}
		// Keep the matcher if it still has hooks
		if len(keptHooks) > 0 {
			m.Hooks = keptHooks
			kept = append(kept, m)
		}
	}

	if len(kept) > 0 {
		hooks[event] = kept
	} else {
		delete(hooks, event)
	}
}

// isAthenaHook returns true if the command is an Athena hook.
func isAthenaHook(command string) bool {
	return strings.HasPrefix(command, "ath hooks ")
}
