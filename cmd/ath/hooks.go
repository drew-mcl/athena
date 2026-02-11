package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/drewfead/athena/internal/hooks"
)

// runEnable installs Athena hooks and agent archetypes.
func runEnable() error {
	root, err := detectProjectRoot()
	if err != nil {
		return fmt.Errorf("could not detect project root: %w", err)
	}

	// Install hooks
	if err := hooks.Enable(root); err != nil {
		return fmt.Errorf("failed to enable hooks: %w", err)
	}

	fmt.Printf("%s%s%s Athena hooks enabled in %s\n", green, checkMark, reset, hooks.SettingsPath(root))
	fmt.Printf("  Events: SessionStart, Stop, SessionEnd\n\n")

	// Install agent archetypes
	installed, err := hooks.InstallAgents(root)
	if err != nil {
		return fmt.Errorf("failed to install agent archetypes: %w", err)
	}

	if len(installed) > 0 {
		fmt.Printf("%s%s%s Agent archetypes installed in %s\n", green, checkMark, reset, hooks.AgentsPath(root))
		for _, name := range installed {
			fmt.Printf("  - %s\n", name)
		}
	} else {
		fmt.Printf("%sℹ%s  Agent archetypes already installed (skipped to preserve customizations)\n", gray, reset)
	}

	return nil
}

// runDisable removes Athena hooks and optionally agent archetypes.
func runDisable(removeAgents bool) error {
	root, err := detectProjectRoot()
	if err != nil {
		return fmt.Errorf("could not detect project root: %w", err)
	}

	// Disable hooks
	if err := hooks.Disable(root); err != nil {
		return fmt.Errorf("failed to disable hooks: %w", err)
	}

	fmt.Printf("%s%s%s Athena hooks disabled\n", green, checkMark, reset)

	// Remove agent archetypes if requested
	if removeAgents {
		removed, err := hooks.RemoveAgents(root)
		if err != nil {
			return fmt.Errorf("failed to remove agent archetypes: %w", err)
		}

		if len(removed) > 0 {
			fmt.Printf("%s%s%s Agent archetypes removed\n", green, checkMark, reset)
			for _, name := range removed {
				fmt.Printf("  - %s\n", name)
			}
		} else {
			fmt.Printf("%sℹ%s  No Athena agent archetypes found to remove\n", gray, reset)
		}
	} else {
		fmt.Printf("%sℹ%s  Agent archetypes preserved (use --agents to remove)\n", gray, reset)
	}

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
