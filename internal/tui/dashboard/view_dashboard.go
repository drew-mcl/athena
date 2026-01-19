package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/layout"
)

func (m Model) renderProjects() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	// Projects table - overview of all projects
	projectTable := layout.NewTable([]layout.Column{
		{Header: "PROJECT", MinWidth: 15, MaxWidth: 30, Flex: 2},
		{Header: "WORKTREES", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "AGENTS", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "STATUS", MinWidth: 12, MaxWidth: 18, Flex: 1},
	})
	projectTable.SetWidth(m.width)

	b.WriteString(projectTable.RenderHeader())
	b.WriteString("\n\n")

	if len(m.projects) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No projects found. Configure base_dirs in ~/.config/athena/config.yaml"))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.projects), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.projects) {
		end = len(m.projects)
	}

	for i := scroll.Offset; i < end; i++ {
		project := m.projects[i]
		row := m.renderProjectRow(project, projectTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(m.projects) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderProjectRow(project string, table *layout.Table, selected bool) string {
	// Count worktrees for this project
	wtCount := 0
	for _, wt := range m.worktrees {
		if wt.Project == project {
			wtCount++
		}
	}

	// Count agents for this project
	agentCount := 0
	runningCount := 0
	for _, a := range m.agents {
		if a.Project == project {
			agentCount++
			if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
				runningCount++
			}
		}
	}

	// Determine status summary
	status := tui.StyleMuted.Render("idle")
	if runningCount > 0 {
		status = tui.StyleSuccess.Render(fmt.Sprintf("%d running", runningCount))
	} else if agentCount > 0 {
		status = tui.StyleNeutral.Render("agents idle")
	}

	values := []string{
		project,
		fmt.Sprintf("%d", wtCount),
		fmt.Sprintf("%d", agentCount),
		status,
	}

	return table.RenderRow(values, selected)
}

func (m Model) renderAllWorktrees() string {
	contentHeight := layout.ContentHeight(m.height)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         m.worktreeTable,
		TotalItems:    len(m.worktrees),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No worktrees found. Configure base_dirs in ~/.config/athena/config.yaml",
		RowRenderer: func(index int, selected bool) string {
			return m.renderGlobalWorktreeRow(m.worktrees[index], m.worktreeTable, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderGlobalWorktreeRow(wt *control.WorktreeInfo, table *layout.Table, selected bool) string {
	// Determine status based on worktree lifecycle and agent
	status := tui.StyleMuted.Render("idle")
	if wt.WTStatus == "merged" {
		status = tui.StyleNeutral.Render("merged")
	} else if wt.WTStatus == "stale" {
		status = tui.StyleWarning.Render("stale")
	} else if wt.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == wt.AgentID {
				status = tui.StatusStyle(a.Status).Render(a.Status)
				break
			}
		}
	}

	// Summary - use description as fallback if no plan summary
	summary := wt.Summary
	if summary == "" && wt.Description != "" {
		summary = wt.Description
	}
	if summary == "" {
		summary = tui.StyleMuted.Render("—")
	}

	// Columns: BRANCH, SUMMARY, STATUS, GIT
	values := []string{
		wt.Branch,
		summary,
		status,
		wt.Status, // Git status
	}

	row := table.RenderRow(values, selected)

	// Render merged worktrees in muted style (unless selected)
	if wt.WTStatus == "merged" && !selected {
		row = tui.StyleMuted.Render(row)
	}

	return row
}

func (m Model) renderAllJobs() string {
	contentHeight := layout.ContentHeight(m.height)
	return layout.RenderTableList(layout.TableListOptions{
		Table:         m.jobTable,
		TotalItems:    len(m.jobs),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No jobs. Enter a project and press [n] to create one.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderGlobalJobRow(m.jobs[index], index, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderGlobalJobRow(job *control.JobInfo, idx int, selected bool) string {
	agentIcon := tui.StyleMuted.Render("─")
	if job.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == job.AgentID {
				agentIcon = tui.StatusStyle(a.Status).Render(tui.StatusIcons[a.Status])
				break
			}
		}
	}

	values := []string{
		fmt.Sprintf("%d", idx+1),
		job.Project,
		job.NormalizedInput,
		job.Status,
		agentIcon,
	}

	return m.jobTable.RenderRow(values, selected)
}

func (m Model) renderQuestions() string {
	questions := m.questions()

	contentHeight := layout.ContentHeight(m.height)

	// Questions table - simpler than jobs, focused on Q&A
	questionTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "QUESTION", MinWidth: 40, MaxWidth: 0, Flex: 4},
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})
	questionTable.SetWidth(m.width)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         questionTable,
		TotalItems:    len(questions),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No questions. Press [?] to ask a quick question.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderQuestionRow(questions[index], questionTable, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ▲ %d more", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ▼ %d more", remaining)
		},
	})
}

func (m Model) renderQuestionRow(job *control.JobInfo, table *layout.Table, selected bool) string {
	// Status icon
	icon := tui.StatusStyle(job.Status).Render(tui.StatusIcons[job.Status])

	// Calculate age from created_at
	age := ""
	if t, err := time.Parse(time.RFC3339, job.CreatedAt); err == nil {
		age = formatDuration(time.Since(t))
	}

	values := []string{
		icon,
		job.NormalizedInput,
		age,
	}

	return table.RenderRow(values, selected)
}
