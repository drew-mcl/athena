package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var trustMu sync.Mutex

// EnsureWorkspaceTrusted adds the given directory to Claude Code's trusted
// directories in ~/.claude/settings.local.json. This prevents the workspace
// trust dialog from appearing when spawning agents in new directories.
//
// This is a no-op if the directory is already trusted.
func EnsureWorkspaceTrusted(dir string) error {
	trustMu.Lock()
	defer trustMu.Unlock()

	settingsPath, err := settingsLocalPath()
	if err != nil {
		return err
	}

	raw, err := readSettingsLocal(settingsPath)
	if err != nil {
		return err
	}

	// Check if already trusted
	dirs := trustedDirs(raw)
	for _, d := range dirs {
		if d == dir {
			return nil
		}
	}

	// Add and write back
	dirs = append(dirs, dir)
	if err := setTrustedDirs(raw, dirs); err != nil {
		return err
	}
	return writeSettingsLocal(settingsPath, raw)
}

func settingsLocalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.local.json"), nil
}

// readSettingsLocal reads the settings file, preserving all keys.
func readSettingsLocal(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]json.RawMessage), nil
		}
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return make(map[string]json.RawMessage), nil
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	return raw, nil
}

func writeSettingsLocal(path string, raw map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0644)
}

func trustedDirs(raw map[string]json.RawMessage) []string {
	dirBytes, ok := raw["trustedDirectories"]
	if !ok {
		return nil
	}
	var dirs []string
	if err := json.Unmarshal(dirBytes, &dirs); err != nil {
		return nil
	}
	return dirs
}

func setTrustedDirs(raw map[string]json.RawMessage, dirs []string) error {
	b, err := json.Marshal(dirs)
	if err != nil {
		return err
	}
	raw["trustedDirectories"] = json.RawMessage(b)
	return nil
}
