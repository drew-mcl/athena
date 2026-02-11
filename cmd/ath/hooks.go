package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/drewfead/athena/internal/hooks"
)

// runEnable installs Athena hooks into .claude/settings.json.
func runEnable() error {
	root, err := detectProjectRoot()
	if err != nil {
		return fmt.Errorf("could not detect project root: %w", err)
	}

	if err := hooks.Enable(root); err != nil {
		return fmt.Errorf("failed to enable hooks: %w", err)
	}

	fmt.Printf("%s%s%s Athena hooks enabled in %s\n", green, checkMark, reset, hooks.SettingsPath(root))
	fmt.Printf("  Events: SessionStart, Stop, SessionEnd\n")
	return nil
}

// runDisable removes Athena hooks from .claude/settings.json.
func runDisable() error {
	root, err := detectProjectRoot()
	if err != nil {
		return fmt.Errorf("could not detect project root: %w", err)
	}

	if err := hooks.Disable(root); err != nil {
		return fmt.Errorf("failed to disable hooks: %w", err)
	}

	fmt.Printf("%s%s%s Athena hooks disabled\n", green, checkMark, reset)
	return nil
}

// detectProjectRoot walks up from cwd to find a .git directory.
func detectProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fallback to cwd
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return cwd, nil
}
