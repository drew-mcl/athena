package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/layout"
)

const (
	scrollMoreUp   = "   ▲ %d more"
	scrollMoreDown = "   ▼ %d more"
)

func (m Model) renderWorktrees() string {
	wts := m.projectWorktrees()
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	if len(wts) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No worktrees for this project."))
		b.WriteString("\n")
		return b.String()
	}

	// Multi-line rows - each worktree takes 4 lines (3 content + 1 blank)
	rowHeight := 4
	visibleRows := contentHeight / rowHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(wts), m.selected, visibleRows)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf(scrollMoreUp, scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(wts) {
		end = len(wts)
	}

	for i := scroll.Offset; i < end; i++ {
		wt := wts[i]
		row := m.renderWorktreeCard(wt, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(wts) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf(scrollMoreDown, remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderAgents() string {
	agents := m.projectAgents()
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	if len(agents) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No agents running on this project."))
		b.WriteString("\n")
		return b.String()
	}

	// Count active agents
	activeCount := 0
	for _, a := range agents {
		if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
			activeCount++
		}
	}

	// Header with count
	header := "AGENTS"
	if activeCount > 0 {
		header += fmt.Sprintf("  %s", tui.StyleAccent.Render(fmt.Sprintf("%d active", activeCount)))
	} else {
		header += fmt.Sprintf("  %s", tui.StyleMuted.Render(fmt.Sprintf("%d total", len(agents))))
	}
	b.WriteString(tui.StyleHeader.Render("  " + header))
	b.WriteString("\n\n")

	// Multi-line rows - each agent takes 3 lines + 1 blank
	rowHeight := 4
	visibleRows := (contentHeight - 3) / rowHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(agents), m.selected, visibleRows)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ... %d more above", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(agents) {
		end = len(agents)
	}

	for i := scroll.Offset; i < end; i++ {
		agent := agents[i]
		row := m.renderAgentCard(agent, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(agents) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ... %d more below", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

// renderAgentCard renders a multi-line agent card for project view.
func (m Model) renderAgentCard(agent *control.AgentInfo, selected bool) string {
	var lines []string

	// Line 1: Status + archetype + worktree + status pill
	line1 := m.renderAgentCardLine1(agent)
	lines = append(lines, line1)

	// Line 2: Activity description
	line2 := m.renderAgentCardLine2(agent)
	lines = append(lines, line2)

	// Line 3: Metrics
	line3 := m.renderAgentCardLine3(agent)
	lines = append(lines, line3)

	// Blank line for spacing
	lines = append(lines, "")

	// Build with selection indicator
	var sb strings.Builder
	indicator := "  "
	if selected {
		indicator = tui.StyleAccent.Render("> ")
	}

	for i, line := range lines {
		if i == 0 {
			sb.WriteString(indicator)
		} else {
			sb.WriteString("  ")
		}
		if selected && i == 0 {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Render(line))
		} else {
			sb.WriteString(line)
		}
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (m Model) renderAgentCardLine1(agent *control.AgentInfo) string {
	var sb strings.Builder

	// Status indicator - special handling for planner with plan ready
	statusChar := agentStatusIndicator(agent)
	sb.WriteString(statusChar)
	sb.WriteString(" ")

	// Archetype
	sb.WriteString(agent.Archetype)
	sb.WriteString("  ")

	// Worktree name
	wtName := filepath.Base(agent.WorktreePath)
	sb.WriteString(tui.StyleMuted.Render(wtName))

	// Status pill
	statusText := agentStatusText(agent)
	statusStyle := agentStatusStyle(agent)

	sb.WriteString("  ")
	sb.WriteString(tui.StyleMuted.Render("[ "))
	sb.WriteString(statusStyle.Render(fmt.Sprintf("%-10s", statusText)))
	sb.WriteString(tui.StyleMuted.Render(" ]"))

	return sb.String()
}

func agentStatusIndicator(agent *control.AgentInfo) string {
	if agent.Archetype == "planner" && agent.Status == "completed" {
		switch agent.PlanStatus {
		case "draft":
			return tui.StyleWarning.Render("!")
		case "approved":
			return tui.StyleInfo.Render(">")
		}
	}
	statusChar := tui.StatusIcons[agent.Status]
	if statusChar == "" {
		statusChar = "-"
	}
	return tui.StatusStyle(agent.Status).Render(statusChar)
}

func agentStatusText(agent *control.AgentInfo) string {
	if agent.Archetype == "planner" && agent.Status == "completed" {
		switch agent.PlanStatus {
		case "draft":
			return "PLAN READY"
		case "approved":
			return "APPROVED"
		}
	}
	return strings.ToUpper(agent.Status)
}

func agentStatusStyle(agent *control.AgentInfo) lipgloss.Style {
	switch agent.Status {
	case "running", "executing":
		return tui.StyleSuccess
	case "planning":
		return tui.StyleInfo
	case "awaiting":
		return tui.StyleWarning
	case "crashed":
		return tui.StyleDanger
	case "completed":
		return plannerCompletedStyle(agent.PlanStatus)
	default:
		return tui.StyleMuted
	}
}

func plannerCompletedStyle(planStatus string) lipgloss.Style {
	switch planStatus {
	case "draft":
		return tui.StyleWarning
	case "approved":
		return tui.StyleInfo
	default:
		return tui.StyleMuted
	}
}

func (m Model) renderAgentCardLine2(agent *control.AgentInfo) string {
	activity := m.formatAgentActivity(agent)
	if activity == "" {
		activity = tui.StyleMuted.Render("No activity recorded")
	}
	return "  " + activity
}

func (m Model) renderAgentCardLine3(agent *control.AgentInfo) string {
	var parts []string

	// Tokens
	tokens := "-"
	if agent.Metrics != nil && agent.Metrics.TotalTokens > 0 {
		tokens = formatCompactNumber(agent.Metrics.TotalTokens)
	}
	parts = append(parts, tui.StyleMuted.Render("tokens: ")+tokens)

	// Cache
	cache := "-"
	if agent.Metrics != nil && agent.Metrics.CacheReads > 0 {
		cache = tui.StyleSuccess.Render(formatCompactNumber(agent.Metrics.CacheReads))
	}
	parts = append(parts, tui.StyleMuted.Render("cache: ")+cache)

	// Tools
	tools := "-"
	if agent.Metrics != nil && agent.Metrics.ToolUseCount > 0 {
		tools = fmt.Sprintf("%d", agent.Metrics.ToolUseCount)
	}
	parts = append(parts, tui.StyleMuted.Render("tools: ")+tools)

	// Age
	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))
	parts = append(parts, tui.StyleMuted.Render("age: ")+age)

	return "  " + strings.Join(parts, "  |  ")
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

// agentTaskInfo returns task list name and in-progress task count for an agent.
func (m Model) agentTaskInfo(agent *control.AgentInfo) (listName string, inProgress int, total int) {
	// Find task list matching agent's worktree path
	for _, list := range m.taskLists {
		if list.Path == agent.WorktreePath {
			listName = list.Name
			total = list.TaskCount
			break
		}
	}

	// Count in-progress tasks owned by this agent
	for _, task := range m.claudeTasks {
		if task.Owner == agent.ID && task.Status == "in_progress" {
			inProgress++
		}
	}

	return listName, inProgress, total
}

// formatAgentActivity returns activity with task info if available.
func (m Model) formatAgentActivity(agent *control.AgentInfo) string {
	activity := formatActivity(agent)

	// Add task info if agent has an associated task list
	listName, inProgress, total := m.agentTaskInfo(agent)
	if listName != "" {
		taskInfo := fmt.Sprintf("[%s", listName)
		if total > 0 {
			if inProgress > 0 {
				taskInfo += fmt.Sprintf(": %d/%d", inProgress, total)
			} else {
				taskInfo += fmt.Sprintf(": %d", total)
			}
		}
		taskInfo += "]"
		return tui.StyleMuted.Render(taskInfo) + " " + activity
	}

	return activity
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
			return fmt.Sprintf(scrollMoreUp, offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf(scrollMoreDown, remaining)
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
			return fmt.Sprintf(scrollMoreUp, offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf(scrollMoreDown, remaining)
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
