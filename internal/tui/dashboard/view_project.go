package dashboard

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/layout"
)

func (m Model) renderWorktrees() string {
	wts := m.projectWorktrees()

	contentHeight := layout.ContentHeight(m.height)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         m.worktreeTable,
		TotalItems:    len(wts),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No worktrees for this project.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderGlobalWorktreeRow(wts[index], m.worktreeTable, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderAgents() string {
	agents := m.projectAgents()

	contentHeight := layout.ContentHeight(m.height)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         m.agentTable,
		TotalItems:    len(agents),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No agents running.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderAgentRow(agents[index], m.agentTable, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderAgentRow(agent *control.AgentInfo, table *layout.Table, selected bool) string {
	// Determine status icon - override for completed planners with pending plans
	var icon string
	if agent.Archetype == "planner" && agent.Status == "completed" && agent.PlanStatus == "draft" {
		// Plan ready - use warning icon to draw attention
		icon = tui.StyleWarning.Render("!")
	} else if agent.Archetype == "planner" && agent.Status == "completed" && agent.PlanStatus == "approved" {
		// Plan approved - use info icon
		icon = tui.StyleInfo.Render("»")
	} else {
		icon = tui.StatusStyle(agent.Status).Render(tui.StatusIcons[agent.Status])
	}
	wtName := filepath.Base(agent.WorktreePath)
	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))

	// Look up worktree summary for this agent
	summary := ""
	for _, wt := range m.worktrees {
		if wt.Path == agent.WorktreePath {
			summary = wt.Summary
			if summary == "" && wt.Description != "" {
				summary = wt.Description
			}
			break
		}
	}
	if summary == "" {
		summary = tui.StyleMuted.Render("—")
	}

	// Columns: ST, IMPL, TYPE, WORKTREE, SUMMARY, ACTIVITY, AGE
	values := []string{
		icon,
		tui.StyleMuted.Render("Claude Code"), // Default harness (TODO: dynamic)
		agent.Archetype,
		wtName,
		summary,
		formatActivity(agent),
		age,
	}

	return table.RenderRow(values, selected)
}

// Fun status messages by agent state
var funStatusMessages = map[string][]string{
	"planning": {
		"Brainstorming...",
		"Calculating...",
		"Wondering...",
		"Pondering the orb...",
		"Strategizing...",
		"Connecting dots...",
		"Reviewing the plan...",
		"Consulting the oracle...",
		"Simulating outcomes...",
		"Thinking deeply...",
		"Analyzing requirements...",
		"Mapping the territory...",
		"Sketching ideas...",
		"Formulating hypothesis...",
		"Checking the map...",
		"Plotting course...",
		"Synthesizing data...",
		"Optimizing route...",
		"Designing solution...",
		"Architecting...",
		"Dreaming up code...",
		"Consulting rubber duck...",
		"Parsing intent...",
		"Decoding matrix...",
		"Checking specifications...",
	},
	"executing": {
		"Grafting...",
		"Putting in the work...",
		"Doing your work...",
		"Writing code...",
		"Crushing tickets...",
		"Shipping features...",
		"Generating value...",
		"Refactoring reality...",
		"Compiling success...",
		"Executing order 66...",
		"Applying fixes...",
		"Hammering the keyboard...",
		"Typing furiously...",
		"Injecting logic...",
		"Polishing pixels...",
		"Wiring circuits...",
		"Building the future...",
		"Deploying brilliance...",
		"Making it happen...",
		"Crunching bits...",
		"Assembling bytes...",
		"Weaving software...",
		"Crafting elegance...",
		"Solving puzzles...",
		"Getting it done...",
	},
	"running": {
		"Working...",
		"Busy...",
		"On the job...",
		"Processing...",
		"Handling it...",
		"In the zone...",
		"Flow state...",
		"Running cycles...",
		"Spinning up...",
		"Active...",
	},
}

// getFunStatus returns a consistent random message based on agent ID and state.
// We use the ID as a seed so the message doesn't flicker on every render frame.
func getFunStatus(id, state string) string {
	msgs, ok := funStatusMessages[state]
	if !ok || len(msgs) == 0 {
		return ""
	}

	// Simple hash of ID to pick a message
	hash := 0
	for _, c := range id {
		hash += int(c)
	}

	// Add state length to vary it when state changes
	hash += len(state)

	// Use time (minute) to rotate messages occasionally but not too fast
	// This keeps it "alive" but readable
	hash += time.Now().Minute()

	idx := hash % len(msgs)
	return msgs[idx]
}

// formatActivity returns a human-readable description of what the agent is doing.
// If LastActivity is populated, it shows that with a relative time suffix.
// Otherwise, it falls back to descriptive status messages.
func formatActivity(agent *control.AgentInfo) string {
	// Special handling for completed planner agents with plan status
	if agent.Archetype == "planner" && agent.Status == "completed" {
		switch agent.PlanStatus {
		case "draft":
			return tui.StyleWarning.Render("Plan ready - [p] view")
		case "approved":
			return tui.StyleInfo.Render("Approved - [X] execute")
		case "executing":
			return "Executor running..."
		case "completed":
			return tui.StyleMuted.Render("Plan executed")
		case "pending":
			return "Waiting for plan..."
		}
	}

	if agent.LastActivity != "" {
		// Show activity with relative time if recent
		if agent.LastActivityTime != "" {
			activityTime := parseCreatedAt(agent.LastActivityTime)
			if !activityTime.IsZero() && time.Since(activityTime) < 5*time.Minute {
				relTime := formatDuration(time.Since(activityTime))
				return fmt.Sprintf("%s (%s ago)", agent.LastActivity, relTime)
			}
		}
		return agent.LastActivity
	}

	// Fallback to status-based descriptions with fun flavor
	funMsg := getFunStatus(agent.ID, agent.Status)
	if funMsg != "" {
		return tui.StyleDanger.Render(funMsg) // "nice pink" as requested
	}

	switch agent.Status {
	case "awaiting":
		return "Waiting for user input"
	case "crashed":
		return tui.StyleDanger.Render("Process crashed - needs restart")
	case "stopped":
		return "Stopped"
	case "completed":
		return tui.StyleMuted.Render("—")
	default:
		return agent.Status
	}
}

func (m Model) renderNotes() string {
	contentHeight := layout.ContentHeight(m.height)
	notes := m.projectNotes()

	// Notes table - no MaxWidth so notes never truncate
	noteTable := layout.NewTable([]layout.Column{
		{Header: "✓", MinWidth: 3, MaxWidth: 3, Flex: 0},
		{Header: "NOTE", MinWidth: 45, MaxWidth: 0, Flex: 3}, // MaxWidth 0 = unlimited
	})
	noteTable.SetWidth(m.width)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         noteTable,
		TotalItems:    len(notes),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No active notes. Quick ideas and todos go here.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderNoteRow(notes[index], noteTable, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderNoteRow(note *control.NoteInfo, table *layout.Table, selected bool) string {
	check := "○"
	if note.Done {
		check = "●"
	}

	values := []string{
		check,
		note.Content,
	}

	row := table.RenderRow(values, selected)
	if note.Done && !selected {
		return tui.StyleMuted.Render(row)
	}
	return row
}

func (m Model) renderTasks() string {
	tasks := m.projectTasks()

	contentHeight := layout.ContentHeight(m.height)

	// Tasks table (short-lived jobs)
	taskTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "TASK", MinWidth: 35, MaxWidth: 60, Flex: 3},
		{Header: "STATUS", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})
	taskTable.SetWidth(m.width)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         taskTable,
		TotalItems:    len(tasks),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No tasks. Press [n] to create one.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderTaskRow(tasks[index], taskTable, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderTaskRow(task *control.JobInfo, table *layout.Table, selected bool) string {
	icon := tui.StatusStyle(task.Status).Render(tui.StatusIcons[task.Status])

	age := ""
	if t, err := time.Parse(time.RFC3339, task.CreatedAt); err == nil {
		age = formatDuration(time.Since(t))
	}

	// Style the task description based on status
	taskDesc := task.NormalizedInput
	if task.Status == "completed" {
		taskDesc = tui.StyleMuted.Render(taskDesc)
	} else if task.Status == "running" || task.Status == "executing" {
		taskDesc = tui.StyleAccent.Render(taskDesc)
	}

	values := []string{
		icon,
		taskDesc,
		tui.StatusStyle(task.Status).Render(task.Status),
		age,
	}

	return table.RenderRowStyled(values, selected)
}
