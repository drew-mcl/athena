package dashboard

import (
	"strings"

	"github.com/drewfead/athena/internal/tui"
)

// Action defines a keyboard action with its context
type Action struct {
	Key     string  // The key binding (e.g., "n", "enter", "L")
	Label   string  // Display label (e.g., "new", "drill", "logs")
	Tabs    []Tab   // Which tabs this applies to (nil = all tabs)
	Levels  []Level // Which levels this applies to (nil = all levels)
	Tooltip string  // Extended description shown on unavailable action
}

// allActions defines all available actions with their context
var allActions = []Action{
	// Navigation
	{Key: "enter", Label: "drill", Tabs: []Tab{TabProjects, TabWorktrees}, Levels: []Level{LevelDashboard}},
	{Key: "enter", Label: "open", Tabs: []Tab{TabJobs, TabAgents, TabTasks, TabQuestions}, Levels: nil},
	{Key: "tab", Label: "switch", Tabs: nil, Levels: nil},
	{Key: "j/k", Label: "move", Tabs: nil, Levels: nil},

	// Global actions
	{Key: "w", Label: "workflow", Tabs: nil, Levels: nil, Tooltip: "Cycle workflow mode (automatic/approve/manual)"},

	// Creation
	{Key: "n", Label: "new", Tabs: nil, Levels: nil},
	{Key: "?", Label: "ask", Tabs: []Tab{TabWorktrees, TabJobs, TabAgents, TabTasks, TabQuestions}, Levels: nil},
	{Key: "N", Label: "normalize", Tabs: []Tab{TabProjects}, Levels: []Level{LevelDashboard}, Tooltip: "Organize repos into correct structure"},

	// Agent/Worktree actions
	{Key: "a", Label: "attach", Tabs: []Tab{TabWorktrees, TabAgents}, Levels: nil, Tooltip: "Attach requires a worktree or agent selection"},
	{Key: "A", Label: "approve", Tabs: []Tab{TabAgents}, Levels: nil, Tooltip: "Approve a draft plan from planner agent"},
	{Key: "c", Label: "cleanup", Tabs: []Tab{TabWorktrees}, Levels: nil, Tooltip: "Remove merged worktree"},
	{Key: "D", Label: "abandon", Tabs: []Tab{TabWorktrees}, Levels: nil, Tooltip: "Abandon worktree (force delete with branch)"},
	{Key: "C", Label: "context", Tabs: []Tab{TabWorktrees, TabAgents}, Levels: nil, Tooltip: "View agent context (blackboard + state)"},
	{Key: "e", Label: "edit", Tabs: []Tab{TabWorktrees, TabAgents}, Levels: nil, Tooltip: "Edit requires a worktree or agent selection"},
	{Key: "L", Label: "logs", Tabs: []Tab{TabWorktrees, TabAgents, TabTasks}, Levels: nil, Tooltip: "Logs requires a worktree with agent or agent selection"},
	{Key: "M", Label: "merge", Tabs: []Tab{TabWorktrees}, Levels: nil, Tooltip: "Merge to main locally"},
	{Key: "p", Label: "plan", Tabs: []Tab{TabWorktrees, TabAgents}, Levels: nil, Tooltip: "View implementation plan"},
	{Key: "P", Label: "pr", Tabs: []Tab{TabWorktrees}, Levels: nil, Tooltip: "Push and create PR for review"},
	{Key: "r", Label: "retry", Tabs: []Tab{TabAgents}, Levels: nil, Tooltip: "Retry/respawn a crashed or completed planner agent"},
	{Key: "s", Label: "shell", Tabs: []Tab{TabWorktrees}, Levels: nil, Tooltip: "Shell requires a worktree selection"},
	{Key: "v", Label: "view", Tabs: []Tab{TabWorktrees, TabAgents, TabTasks}, Levels: nil, Tooltip: "View requires a worktree with agent or agent selection"},
	{Key: "x", Label: "kill", Tabs: []Tab{TabWorktrees, TabAgents, TabTasks}, Levels: nil, Tooltip: "Kill requires a worktree with agent or agent selection"},
	{Key: "X", Label: "execute", Tabs: []Tab{TabAgents}, Levels: nil, Tooltip: "Spawn executor for approved plan"},

	// Notes actions (drill-in only)
	{Key: "x", Label: "toggle", Tabs: []Tab{TabNotes}, Levels: []Level{LevelProject}},
	{Key: "space", Label: "toggle", Tabs: []Tab{TabNotes}, Levels: []Level{LevelProject}},
	{Key: "f", Label: "promote", Tabs: []Tab{TabNotes}, Levels: []Level{LevelProject}, Tooltip: "Promote note to feature/worktree"},
	{Key: "d", Label: "delete", Tabs: []Tab{TabNotes}, Levels: []Level{LevelProject}},

	// Exit
	{Key: "q", Label: "quit", Tabs: nil, Levels: []Level{LevelDashboard}},
	{Key: "q", Label: "back", Tabs: nil, Levels: []Level{LevelProject}},
}

// GetAvailableActions returns actions applicable to the current tab and level
func GetAvailableActions(tab Tab, level Level) []Action {
	seen := make(map[string]bool)
	var result []Action

	for _, a := range allActions {
		// Check tab applicability
		if a.Tabs != nil && !containsTab(a.Tabs, tab) {
			continue
		}

		// Check level applicability
		if a.Levels != nil && !containsLevel(a.Levels, level) {
			continue
		}

		// Deduplicate by key (keep first match)
		if seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		result = append(result, a)
	}

	return result
}

// FormatHelp generates the help string for the current context
func FormatHelp(tab Tab, level Level) string {
	actions := GetAvailableActions(tab, level)

	// Group actions for display (skip navigation keys like j/k, tab)
	displayActions := []Action{}
	for _, a := range actions {
		if a.Key == "j/k" || a.Key == "tab" {
			continue
		}
		displayActions = append(displayActions, a)
	}

	var parts []string
	for _, a := range displayActions {
		parts = append(parts, formatActionKey(a))
	}

	return strings.Join(parts, "  ")
}

// formatActionKey formats a single action for help display
func formatActionKey(a Action) string {
	key := a.Key
	// Format special keys
	switch key {
	case "enter":
		key = "⏎"
	case "space":
		key = "␣"
	}
	return tui.StyleHelpKey.Render("["+key+"]") + tui.StyleHelp.Render(a.Label)
}

// IsActionAvailable checks if an action is available in the current context
func IsActionAvailable(key string, tab Tab, level Level) bool {
	for _, a := range allActions {
		if a.Key != key {
			continue
		}

		// Check tab
		if a.Tabs != nil && !containsTab(a.Tabs, tab) {
			continue
		}

		// Check level
		if a.Levels != nil && !containsLevel(a.Levels, level) {
			continue
		}

		return true
	}
	return false
}

// GetActionTooltip returns the tooltip for an unavailable action
func GetActionTooltip(key string, tab Tab, level Level) string {
	for _, a := range allActions {
		if a.Key == key && a.Tooltip != "" {
			return a.Tooltip
		}
	}
	return "Action not available in this context"
}

func containsTab(tabs []Tab, tab Tab) bool {
	for _, t := range tabs {
		if t == tab {
			return true
		}
	}
	return false
}

func containsLevel(levels []Level, level Level) bool {
	for _, l := range levels {
		if l == level {
			return true
		}
	}
	return false
}
