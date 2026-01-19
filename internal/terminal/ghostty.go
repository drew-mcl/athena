// Package terminal provides terminal emulator integration.
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/executil"
)

// Ghostty provides integration with the Ghostty terminal emulator.
type Ghostty struct {
	// SpawnCommand can override the default spawn behavior
	SpawnCommand string
}

// NewGhostty creates a new Ghostty integration.
func NewGhostty() *Ghostty {
	return &Ghostty{}
}

// OpenTab opens a new Ghostty tab with the given command.
// Uses AppleScript on macOS to send cmd+t then run the command.
func (g *Ghostty) OpenTab(workDir string, command ...string) error {
	if runtime.GOOS == "darwin" {
		return g.openTabMacOS(workDir, command...)
	}
	// Fallback: open new window
	return g.openWindow(workDir, command...)
}

// OpenSplit opens a new Ghostty split with the given command.
// Uses AppleScript on macOS to send cmd+d for vertical split.
func (g *Ghostty) OpenSplit(workDir string, command ...string) error {
	if runtime.GOOS == "darwin" {
		return g.openSplitMacOS(workDir, false, command...)
	}
	return g.openWindow(workDir, command...)
}

// OpenSplitHorizontal opens a horizontal split.
func (g *Ghostty) OpenSplitHorizontal(workDir string, command ...string) error {
	if runtime.GOOS == "darwin" {
		return g.openSplitMacOS(workDir, true, command...)
	}
	return g.openWindow(workDir, command...)
}

func (g *Ghostty) openTabMacOS(workDir string, command ...string) error {
	// Build the command to run in the new tab
	cmdStr := g.buildShellCommand(workDir, command...)

	// AppleScript to open new tab and run command
	script := fmt.Sprintf(`
tell application "Ghostty"
    activate
    delay 0.1
    tell application "System Events"
        keystroke "t" using command down
        delay 0.2
        keystroke "%s"
        keystroke return
    end tell
end tell
`, escapeAppleScript(cmdStr))

	cmd, err := executil.Command("osascript", "-e", script)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func (g *Ghostty) openSplitMacOS(workDir string, horizontal bool, command ...string) error {
	cmdStr := g.buildShellCommand(workDir, command...)

	// cmd+d for vertical split, cmd+shift+d for horizontal
	keystroke := `keystroke "d" using command down`
	if horizontal {
		keystroke = `keystroke "d" using {command down, shift down}`
	}

	script := fmt.Sprintf(`
tell application "Ghostty"
    activate
    delay 0.1
    tell application "System Events"
        %s
        delay 0.2
        keystroke "%s"
        keystroke return
    end tell
end tell
`, keystroke, escapeAppleScript(cmdStr))

	cmd, err := executil.Command("osascript", "-e", script)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func (g *Ghostty) openWindow(workDir string, command ...string) error {
	args := []string{}
	if workDir != "" {
		args = append(args, "--working-directory="+workDir)
	}
	if len(command) > 0 {
		args = append(args, "-e")
		args = append(args, command...)
	}
	cmd, err := executil.Command("ghostty", args...)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func (g *Ghostty) buildShellCommand(workDir string, command ...string) string {
	var parts []string

	if workDir != "" {
		parts = append(parts, fmt.Sprintf("cd %q", workDir))
	}

	if len(command) > 0 {
		parts = append(parts, strings.Join(command, " "))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " && ")
}

func escapeAppleScript(s string) string {
	// Escape backslashes and quotes for AppleScript
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// FocusGhostty brings Ghostty to the foreground.
func (g *Ghostty) FocusGhostty() error {
	if runtime.GOOS == "darwin" {
		cmd, err := executil.Command("osascript", "-e", `tell application "Ghostty" to activate`)
		if err != nil {
			return err
		}
		return cmd.Run()
	}
	return nil
}

// SendKeys sends keystrokes to Ghostty (macOS only).
func (g *Ghostty) SendKeys(keys string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("SendKeys only supported on macOS")
	}

	script := fmt.Sprintf(`
tell application "Ghostty"
    activate
    delay 0.1
    tell application "System Events"
        keystroke "%s"
    end tell
end tell
`, escapeAppleScript(keys))

	cmd, err := executil.Command("osascript", "-e", script)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// WaitAndSendKeys waits then sends keystrokes.
func (g *Ghostty) WaitAndSendKeys(delay time.Duration, keys string) error {
	time.Sleep(delay)
	return g.SendKeys(keys)
}

// OpenNvim opens nvim in a new Ghostty tab at the given directory.
func (g *Ghostty) OpenNvim(workDir string, liveReload bool) error {
	nvimArgs := []string{"nvim"}
	// Live reload should be configured in user's nvim config, not passed via CLI
	// See README for recommended autoread configuration
	return g.OpenTab(workDir, nvimArgs...)
}

// OpenNvimWithFile opens nvim on a specific file.
func (g *Ghostty) OpenNvimWithFile(workDir, file string) error {
	return g.OpenTab(workDir, "nvim", file)
}

// AttachToAgent opens a Ghostty tab attached to an agent's Claude session.
func (g *Ghostty) AttachToAgent(workDir, sessionID string) error {
	return g.OpenTab(workDir, "claude", "--resume", sessionID)
}

// OpenAgentLiveView opens a read-only view of an agent's output stream.
// This uses tail -f on the agent's event log.
func (g *Ghostty) OpenAgentLiveView(logFile string) error {
	if logFile == "" {
		return fmt.Errorf("no log file specified")
	}

	// Use a simple tail -f with some formatting
	return g.OpenTab("", "bash", "-c",
		fmt.Sprintf("tail -f %s | jq -r '.type + \": \" + (.content // .name // \"\")'", logFile))
}

// OpenShell opens a plain shell in a new Ghostty tab.
func (g *Ghostty) OpenShell(workDir string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	return g.OpenTab(workDir, shell)
}

// Terminal is the interface for terminal emulator operations.
type Terminal interface {
	OpenTab(workDir string, command ...string) error
	OpenNvim(workDir string, liveReload bool) error
	OpenNvimWithFile(workDir, file string) error
	AttachToAgent(workDir, sessionID string) error
	OpenAgentLiveView(logFile string) error
	OpenShell(workDir string) error
}

// Detect attempts to detect the available terminal emulator.
func Detect() Terminal {
	// Check for Ghostty first (preferred)
	if _, err := exec.LookPath("ghostty"); err == nil {
		return NewGhostty()
	}

	// Could add iTerm, Kitty, WezTerm, etc.
	return NewGhostty() // Default to Ghostty commands
}
