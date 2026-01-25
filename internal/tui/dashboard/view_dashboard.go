package dashboard

import (
	"fmt"
	"path/filepath"
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
	// Count worktrees for this project
	wtCount := 0
	var lastActivity time.Time
	for _, wt := range m.worktrees {
		if wt.Project == project {
			wtCount++
		}
	}

	// Count agents for this project
	agentCount := 0
	runningCount := 0
	awaitingCount := 0
	for _, a := range m.agents {
		if a.Project == project {
			agentCount++
			switch a.Status {
			case "running", "planning", "executing":
				runningCount++
			case "awaiting":
				awaitingCount++
			}
			// Check agent's last activity time
			if a.LastActivityTime != "" {
				if t, err := time.Parse(time.RFC3339, a.LastActivityTime); err == nil && t.After(lastActivity) {
					lastActivity = t
				}
			}
		}
	}

	// Determine status
	status := "idle"
	if awaitingCount > 0 {
		status = "attention"
	} else if runningCount > 0 {
		status = "healthy"
	}

	// Format last active
	lastActiveStr := ""
	if !lastActivity.IsZero() {
		lastActiveStr = formatDuration(time.Since(lastActivity)) + " ago"
	}

	// Use the ProjectCard component
	card := &components.ProjectCard{
		Name:       project,
		Worktrees:  wtCount,
		Agents:     agentCount,
		Running:    runningCount,
		Awaiting:   awaitingCount,
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
	// Determine status based on worktree lifecycle and agent
	status := "IDLE"
	agentCount := 0

	// Check agent status first (most specific)
	for _, a := range m.agents {
		if a.WorktreePath == wt.Path {
			agentCount++
			if a.Status == "running" || a.Status == "executing" {
				status = "RUNNING"
			} else if a.Status == "planning" {
				status = "RUNNING"
			} else if a.Status == "awaiting" {
				status = "ATTENTION"
			}
		}
	}

	// Fallback to worktree status if no active agent
	if agentCount == 0 {
		if wt.WTStatus == "merged" {
			status = "MERGED"
		} else if wt.WTStatus == "stale" {
			status = "ATTENTION"
		}
	}

	// Git status
	gitStatus := "clean"
	if strings.Contains(wt.Status, "dirty") || strings.Contains(wt.Status, "M") {
		gitStatus = "dirty"
	}

	// Summary
	summary := wt.Summary
	if summary == "" && wt.Description != "" {
		summary = wt.Description
	}
	if summary == "" {
		summary = "No description"
	}

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
	// Determine status based on worktree lifecycle and agent
	statusText := "IDLE"
	statusStyle := tui.StyleMuted

	// Check agent status first (most specific)
	agentActive := false
	if wt.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == wt.AgentID {
				agentActive = true
				statusText = strings.ToUpper(a.Status)
				if a.Status == "running" || a.Status == "executing" {
					statusStyle = tui.StylePillActive
				} else if a.Status == "planning" {
					statusStyle = tui.StylePillReview
				} else {
					statusStyle = tui.StatusStyle(a.Status)
				}
				break
			}
		}
	}

	// Fallback to worktree status if no active agent overrides it
	if !agentActive {
		if wt.WTStatus == "merged" {
			statusText = "MERGED"
			statusStyle = tui.StyleNeutral
		} else if wt.WTStatus == "stale" {
			statusText = "STALE"
			statusStyle = tui.StylePillStale
		} else if wt.WTStatus == "active" {
			statusText = "ACTIVE"
			statusStyle = tui.StylePillActive
		}
	}

	// Format as fixed-width pill: [ STATUS    ]
	// Truncate if too long (unlikely with standard statuses)
	if len(statusText) > 9 {
		statusText = statusText[:9]
	}
	paddedStatus := fmt.Sprintf("%-9s", statusText)
	status := tui.StyleMuted.Render("[ ") + statusStyle.Render(paddedStatus) + tui.StyleMuted.Render(" ]")

	// Summary - use description as fallback if no plan summary
	summary := wt.Summary
	if summary == "" && wt.Description != "" {
		summary = wt.Description
	}
	if summary == "" {
		summary = tui.StyleMuted.Render("—")
	}

	// Git status coloring
	gitStatus := wt.Status
	if strings.Contains(gitStatus, "dirty") || strings.Contains(gitStatus, "M") {
		gitStatus = tui.StyleWarning.Render(gitStatus)
	} else if strings.Contains(gitStatus, "clean") {
		gitStatus = tui.StyleSuccess.Render(gitStatus)
	} else {
		gitStatus = tui.StyleMuted.Render(gitStatus)
	}

	// Count agents
	count := 0
	for _, a := range m.agents {
		if a.WorktreePath == wt.Path {
			count++
		}
	}
	agentCount := tui.StyleMuted.Render("-")
	if count > 0 {
		agentCount = tui.StyleInfo.Render(fmt.Sprintf("%d", count)) // Cyan color
	}

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

	// Create a global agent table with project column
	table := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "TYPE", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "PROJECT", MinWidth: 10, MaxWidth: 20, Flex: 1},
		{Header: "WORKTREE", MinWidth: 12, MaxWidth: 25, Flex: 1},
		{Header: "STATUS", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "TOKENS", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "TOOLS", MinWidth: 5, MaxWidth: 6, Flex: 0},
		{Header: "AGE", MinWidth: 4, MaxWidth: 8, Flex: 0},
	})
	table.SetWidth(m.width)

	return layout.RenderTableList(layout.TableListOptions{
		Table:         table,
		TotalItems:    len(m.agents),
		Selected:      m.selected,
		ContentHeight: contentHeight,
		EmptyMessage:  "   No agents running. Start work on a worktree to spawn an agent.",
		RowRenderer: func(index int, selected bool) string {
			return m.renderGlobalAgentRow(m.agents[index], table, selected)
		},
		ScrollUpRenderer: func(offset int) string {
			return fmt.Sprintf("   ... %d more above", offset)
		},
		ScrollDownRenderer: func(remaining int) string {
			return fmt.Sprintf("   ... %d more below", remaining)
		},
	})
}

// renderGlobalAgentRow renders a single agent as a table row.
func (m Model) renderGlobalAgentRow(agent *control.AgentInfo, table *layout.Table, selected bool) string {
	// Status icon with color
	icon := tui.StatusIcons[agent.Status]
	if icon == "" {
		icon = "-"
	}
	styledIcon := tui.StatusStyle(agent.Status).Render(icon)

	// Type (archetype)
	agentType := agent.Archetype

	// Project
	project := agent.ProjectName
	if project == "" {
		project = agent.Project
	}

	// Worktree - use friendly name if available
	worktree := filepath.Base(agent.WorktreePath)

	// Status with color
	statusText := agent.Status
	var statusStyle lipgloss.Style
	switch agent.Status {
	case "running", "executing":
		statusStyle = tui.StyleSuccess
	case "planning":
		statusStyle = tui.StyleInfo
	case "awaiting":
		statusStyle = tui.StyleWarning
	case "crashed":
		statusStyle = tui.StyleDanger
	case "completed":
		statusStyle = tui.StyleNeutral
	default:
		statusStyle = tui.StyleMuted
	}
	styledStatus := statusStyle.Render(statusText)

	// Tokens
	tokens := "-"
	if agent.Metrics != nil && agent.Metrics.TotalTokens > 0 {
		tokens = formatCompactNumber(agent.Metrics.TotalTokens)
	}

	// Tools count
	tools := "-"
	if agent.Metrics != nil && agent.Metrics.ToolUseCount > 0 {
		tools = fmt.Sprintf("%d", agent.Metrics.ToolUseCount)
	}

	// Age
	age := "-"
	if t, err := time.Parse(time.RFC3339, agent.CreatedAt); err == nil {
		age = formatDuration(time.Since(t))
	}

	values := []string{styledIcon, agentType, project, worktree, styledStatus, tokens, tools, age}
	return table.RenderRow(values, selected)
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
	// Status icon - use + for answered, * for pending
	var icon string
	if job.Status == "completed" || job.Answer != "" {
		icon = tui.StyleSuccess.Render("+")
	} else {
		icon = tui.StyleWarning.Render("*")
	}

	// Question text
	question := job.NormalizedInput

	// Answer preview (truncation handled by table)
	answer := job.Answer
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
