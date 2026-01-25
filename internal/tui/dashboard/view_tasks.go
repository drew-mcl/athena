package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/components"
	"github.com/drewfead/athena/internal/tui/layout"
)

// renderClaudeTasks renders the Claude Code Tasks tab at dashboard level.
func (m Model) renderClaudeTasks() string {
	var b strings.Builder
	contentHeight := layout.ContentHeight(m.height)

	// Show task list selector if we have multiple lists
	if len(m.taskLists) > 1 {
		b.WriteString(m.renderTaskListSelector())
		b.WriteString("\n\n")
		contentHeight -= 2
	} else if len(m.taskLists) == 1 {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   List: %s", m.taskLists[0].Name)))
		b.WriteString("\n\n")
		contentHeight -= 2
	}

	// If no task lists, show empty state
	if len(m.taskLists) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No Claude Code task lists found"))
		b.WriteString("\n")
		b.WriteString(tui.StyleMuted.Render("   Tasks appear in ~/.claude/tasks/ when agents use TaskCreate"))
		b.WriteString("\n")
		return b.String()
	}

	// Count tasks by status for summary
	pending, inProgress, completed := 0, 0, 0
	for _, t := range m.claudeTasks {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}

	// Reserve space for summary
	contentHeight -= 3

	if len(m.claudeTasks) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No tasks in this list"))
		b.WriteString("\n")
		return b.String()
	}

	// Multi-line rows - each task takes 3 lines
	rowHeight := 3
	visibleRows := contentHeight / rowHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.claudeTasks), m.selected, visibleRows)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.claudeTasks) {
		end = len(m.claudeTasks)
	}

	for i := scroll.Offset; i < end; i++ {
		task := m.claudeTasks[i]
		row := m.renderTaskCard(task, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(m.claudeTasks) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	// Summary stats
	b.WriteString("\n")
	b.WriteString(tui.Divider(m.width - 4))
	b.WriteString("\n")
	summaryParts := []string{}
	if pending > 0 {
		summaryParts = append(summaryParts, tui.StyleMuted.Render(fmt.Sprintf("%d pending", pending)))
	}
	if inProgress > 0 {
		summaryParts = append(summaryParts, tui.StyleSuccess.Render(fmt.Sprintf("%d in progress", inProgress)))
	}
	if completed > 0 {
		summaryParts = append(summaryParts, tui.StyleNeutral.Render(fmt.Sprintf("%d completed", completed)))
	}
	b.WriteString("   " + strings.Join(summaryParts, " • "))
	b.WriteString("\n")

	return b.String()
}

// renderTaskCard renders a task using the multi-line card format.
func (m Model) renderTaskCard(task *control.TaskInfo, selected bool) string {
	// Calculate age from created_at
	age := formatTaskAge(task.CreatedAt)

	// Get blockers as string slice
	var blockers []string
	for _, b := range task.BlockedBy {
		blockers = append(blockers, b)
	}

	// Subject - use ActiveForm if in progress
	subject := task.Subject
	if task.ActiveForm != "" && task.Status == "in_progress" {
		subject = task.ActiveForm
	}

	row := &components.TaskRow{
		Subject:   subject,
		Status:    task.Status,
		Age:       age,
		BlockedBy: blockers,
		Selected:  selected,
	}

	return row.Render()
}

// renderTaskListSelector renders a horizontal selector for task lists.
func (m Model) renderTaskListSelector() string {
	var parts []string
	parts = append(parts, "   Lists: ")

	for _, list := range m.taskLists {
		isSelected := m.selectedTaskList == list.ID || (m.selectedTaskList == "" && list == m.taskLists[0])

		label := list.Name
		if list.TaskCount > 0 {
			label = fmt.Sprintf("%s (%d)", list.Name, list.TaskCount)
		}

		if isSelected {
			parts = append(parts, tui.StylePillActive.Render(" "+label+" "))
		} else {
			parts = append(parts, tui.StyleMuted.Render(" "+label+" "))
		}
	}

	return strings.Join(parts, " ")
}

// renderClaudeTaskRow renders a single task row.
func (m Model) renderClaudeTaskRow(task *control.TaskInfo, selected bool) string {
	// Status indicator
	statusIcon := taskStatusIcon(task.Status)

	// Subject (truncated if needed)
	subject := task.Subject
	if task.ActiveForm != "" && task.Status == "in_progress" {
		subject = task.ActiveForm
	}

	// List ID (shortened)
	listID := task.ListID
	if len(listID) > 15 {
		listID = listID[:12] + "..."
	}

	// Owner (agent ID shortened)
	owner := task.Owner
	if owner == "" {
		owner = "-"
	} else if len(owner) > 12 {
		owner = owner[:9] + "..."
	}

	// Age
	age := formatTaskAge(task.CreatedAt)

	values := []string{
		statusIcon,
		subject,
		listID,
		owner,
		age,
	}

	return m.taskTable.RenderRow(values, selected)
}

// taskStatusIcon returns an icon for the task status.
func taskStatusIcon(status string) string {
	switch status {
	case "pending":
		return tui.StyleMuted.Render("○")
	case "in_progress":
		return tui.StylePillActive.Render("●")
	case "completed":
		return tui.StyleSuccess.Render("✓")
	default:
		return tui.StyleMuted.Render("?")
	}
}

// formatTaskAge formats a task creation time as a relative age.
func formatTaskAge(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return "-"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
