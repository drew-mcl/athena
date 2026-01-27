package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/components"
	"github.com/drewfead/athena/internal/tui/layout"
)

func (m Model) renderProjects() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	if len(m.projects) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No projects found. Configure base_dirs in ~/.config/athena/config.yaml"))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Card-based layout - each project gets 4 lines (card content + spacing)
	cardHeight := 4
	visibleCards := contentHeight / cardHeight
	if visibleCards < 1 {
		visibleCards = 1
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.projects), m.selected, visibleCards)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible cards
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.projects) {
		end = len(m.projects)
	}

	for i := scroll.Offset; i < end; i++ {
		project := m.projects[i]
		card := m.renderProjectCard(project, i == m.selected)
		b.WriteString(card)
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

func (m Model) renderProjectCard(project string, selected bool) string {
	wtCount := m.projectWorktreeCount(project)
	agentSummary := m.projectAgentSummary(project)
	status := projectStatus(agentSummary)
	lastActiveStr := formatProjectLastActive(agentSummary.lastActivity)

	// Use the ProjectCard component
	card := &components.ProjectCard{
		Name:       project,
		Worktrees:  wtCount,
		Agents:     agentSummary.agentCount,
		Running:    agentSummary.runningCount,
		Awaiting:   agentSummary.awaitingCount,
		CacheRate:  0, // Not readily available
		LastActive: lastActiveStr,
		Status:     status,
		Selected:   selected,
		Width:      m.width - 4, // Leave margin
	}

	return card.Render()
}

// renderProjectRow is kept for backward compatibility but now delegates to card
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
	var b strings.Builder
	contentHeight := layout.ContentHeight(m.height)

	if len(m.worktrees) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No worktrees found. Configure base_dirs in ~/.config/athena/config.yaml"))
		b.WriteString("\n")
		return b.String()
	}

	// Multi-line rows - each worktree takes 3 lines + 1 blank
	rowHeight := 4
	visibleRows := contentHeight / rowHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.worktrees), m.selected, visibleRows)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.worktrees) {
		end = len(m.worktrees)
	}

	for i := scroll.Offset; i < end; i++ {
		wt := m.worktrees[i]
		row := m.renderWorktreeCard(wt, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(m.worktrees) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderWorktreeCard(wt *control.WorktreeInfo, selected bool) string {
	status, agentCount := m.worktreeAgentStatus(wt)
	if agentCount == 0 {
		status = worktreeLifecycleStatus(wt.WTStatus, status)
	}

	gitStatus := "clean"
	if isDirtyGitStatus(wt.Status) {
		gitStatus = "dirty"
	}

	summary := worktreeSummary(wt, "No description")

	// Last active (not readily available, leave empty)
	lastActive := ""

	row := &components.WorktreeRow{
		Branch:     wt.Branch,
		Summary:    summary,
		Project:    wt.Project,
		GitStatus:  gitStatus,
		LastActive: lastActive,
		Status:     status,
		AgentCount: agentCount,
		Selected:   selected,
		Width:      m.width,
	}

	return row.Render()
}

func (m Model) renderGlobalWorktreeRow(wt *control.WorktreeInfo, table *layout.Table, selected bool) string {
	statusText, statusStyle, agentActive := m.worktreeAgentPillStatus(wt)
	if !agentActive {
		statusText, statusStyle = worktreeLifecyclePillStatus(wt.WTStatus, statusText, statusStyle)
	}
	status := formatStatusPill(statusText, statusStyle)

	summary := worktreeSummary(wt, tui.StyleMuted.Render("—"))
	gitStatus := renderGitStatus(wt.Status)
	agentCount := renderWorktreeAgentCount(m.worktreeAgentCount(wt))

	// Columns: BRANCH, SUMMARY, AGENTS, STATUS, GIT
	values := []string{
		wt.Branch,
		summary,
		agentCount,
		status,
		gitStatus,
	}

	row := table.RenderRow(values, selected)

	// Render merged worktrees in muted style (unless selected)
	if wt.WTStatus == "merged" && !selected {
		row = tui.StyleMuted.Render(row)
	}

	return row
}

type projectAgentSummary struct {
	agentCount    int
	runningCount  int
	awaitingCount int
	lastActivity  time.Time
}

func (m Model) projectWorktreeCount(project string) int {
	count := 0
	for _, wt := range m.worktrees {
		if wt.Project == project {
			count++
		}
	}
	return count
}

func (m Model) projectAgentSummary(project string) projectAgentSummary {
	summary := projectAgentSummary{}
	for _, a := range m.agents {
		if a.Project != project {
			continue
		}
		summary.agentCount++
		switch a.Status {
		case "running", "planning", "executing":
			summary.runningCount++
		case "awaiting":
			summary.awaitingCount++
		}
		if a.LastActivityTime == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, a.LastActivityTime); err == nil && t.After(summary.lastActivity) {
			summary.lastActivity = t
		}
	}
	return summary
}

func projectStatus(summary projectAgentSummary) string {
	if summary.awaitingCount > 0 {
		return "attention"
	}
	if summary.runningCount > 0 {
		return "healthy"
	}
	return "idle"
}

func formatProjectLastActive(lastActivity time.Time) string {
	if lastActivity.IsZero() {
		return ""
	}
	return formatDuration(time.Since(lastActivity)) + " ago"
}

func (m Model) worktreeAgentStatus(wt *control.WorktreeInfo) (string, int) {
	status := "IDLE"
	agentCount := 0
	for _, a := range m.agents {
		if a.WorktreePath != wt.Path {
			continue
		}
		agentCount++
		if agentStatus := statusFromAgent(a.Status); agentStatus != "" {
			status = agentStatus
		}
	}
	return status, agentCount
}

func statusFromAgent(status string) string {
	switch status {
	case "running", "executing", "planning":
		return "RUNNING"
	case "awaiting":
		return "ATTENTION"
	default:
		return ""
	}
}

func worktreeLifecycleStatus(wtStatus, fallback string) string {
	switch wtStatus {
	case "merged":
		return "MERGED"
	case "stale":
		return "ATTENTION"
	default:
		return fallback
	}
}

func worktreeSummary(wt *control.WorktreeInfo, emptyValue string) string {
	summary := wt.Summary
	if summary == "" && wt.Description != "" {
		summary = wt.Description
	}
	if summary == "" {
		summary = emptyValue
	}
	return summary
}

func isDirtyGitStatus(status string) bool {
	return strings.Contains(status, "dirty") || strings.Contains(status, "M")
}

func renderGitStatus(status string) string {
	if strings.Contains(status, "dirty") || strings.Contains(status, "M") {
		return tui.StyleWarning.Render(status)
	}
	if strings.Contains(status, "clean") {
		return tui.StyleSuccess.Render(status)
	}
	return tui.StyleMuted.Render(status)
}

func (m Model) worktreeAgentPillStatus(wt *control.WorktreeInfo) (string, lipgloss.Style, bool) {
	if wt.AgentID == "" {
		return "IDLE", tui.StyleMuted, false
	}
	for _, a := range m.agents {
		if a.ID != wt.AgentID {
			continue
		}
		statusText := strings.ToUpper(a.Status)
		return statusText, statusStyleForAgent(a.Status), true
	}
	return "IDLE", tui.StyleMuted, false
}

func statusStyleForAgent(status string) lipgloss.Style {
	switch status {
	case "running", "executing":
		return tui.StylePillActive
	case "planning":
		return tui.StylePillReview
	default:
		return tui.StatusStyle(status)
	}
}

func worktreeLifecyclePillStatus(wtStatus, statusText string, statusStyle lipgloss.Style) (string, lipgloss.Style) {
	switch wtStatus {
	case "merged":
		return "MERGED", tui.StyleNeutral
	case "stale":
		return "STALE", tui.StylePillStale
	case "active":
		return "ACTIVE", tui.StylePillActive
	default:
		return statusText, statusStyle
	}
}

func formatStatusPill(statusText string, statusStyle lipgloss.Style) string {
	if len(statusText) > 9 {
		statusText = statusText[:9]
	}
	paddedStatus := fmt.Sprintf("%-9s", statusText)
	return tui.StyleMuted.Render("[ ") + statusStyle.Render(paddedStatus) + tui.StyleMuted.Render(" ]")
}

func (m Model) worktreeAgentCount(wt *control.WorktreeInfo) int {
	count := 0
	for _, a := range m.agents {
		if a.WorktreePath == wt.Path {
			count++
		}
	}
	return count
}

func renderWorktreeAgentCount(count int) string {
	if count > 0 {
		return tui.StyleInfo.Render(fmt.Sprintf("%d", count))
	}
	return tui.StyleMuted.Render("-")
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

// renderAllAgents renders the global agents view as a table.
func (m Model) renderAllAgents() string {
	contentHeight := layout.ContentHeight(m.height)
	table := m.newAgentTable(agentTableConfig{
		showProject:  true,
		showWorktree: true,
		showActivity: true,
	})

	return layout.RenderTableList(layout.TableListOptions{
		Table:         table,
		TotalItems:    len(m.agents),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No agents running. Start work on a worktree to spawn an agent.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderAgentTableRow(m.agents[index], table, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ... %d more above", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ... %d more below", remaining)
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

	// Create table with question as flex column
	table := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "QUESTION", MinWidth: 30, MaxWidth: 0, Flex: 3}, // 0 = unlimited
		{Header: "ANSWER", MinWidth: 30, MaxWidth: 0, Flex: 2},
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})
	table.SetWidth(m.width)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         table,
		TotalItems:    len(questions),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No questions. Press [?] to ask a quick question.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderQuestionRow(questions[index], table, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ... %d more above", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ... %d more below", remaining)
		},
	})
}

func (m Model) renderQuestionRow(job *control.JobInfo, table *layout.Table, selected bool) string {
	rawAnswer := compactWhitespace(job.Answer)

	// Status icon - use + for answered, * for pending
	var icon string
	if job.Status == "completed" || rawAnswer != "" {
		icon = tui.StyleSuccess.Render("+")
	} else {
		icon = tui.StyleWarning.Render("*")
	}

	// Question text
	question := compactWhitespace(job.NormalizedInput)
	if question == "" {
		question = "-"
	}

	// Answer preview (truncation handled by table)
	answer := rawAnswer
	if answer == "" {
		if job.Status == "completed" {
			answer = tui.StyleMuted.Render("(no answer)")
		} else {
			answer = tui.StyleWarning.Render("thinking...")
		}
	}

	// Calculate age from created_at
	age := ""
	if t, err := time.Parse(time.RFC3339, job.CreatedAt); err == nil {
		age = formatDuration(time.Since(t))
	}

	values := []string{icon, question, answer, age}
	return table.RenderRow(values, selected)
}

func compactWhitespace(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
