package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
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
		b.WriteString(m.taskTable.RenderHeader())
		b.WriteString("\n\n")
		b.WriteString(tui.StyleEmptyState.Render("   No Claude Code task lists found"))
		b.WriteString("\n")
		b.WriteString(tui.StyleMuted.Render("   Tasks appear in ~/.claude/tasks/ when agents use TaskCreate"))
		b.WriteString("\n")
		for i := 3; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Render task table
	return b.String() + layout.RenderTableList(layout.TableListOptions{
		Table:         m.taskTable,
		TotalItems:    len(m.claudeTasks),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No tasks in this list",
		RowRenderer: func(index int, selected bool) string {
			return m.renderClaudeTaskRow(m.claudeTasks[index], selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
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
