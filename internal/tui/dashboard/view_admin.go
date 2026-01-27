package dashboard

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/components"
	"github.com/drewfead/athena/internal/tui/layout"
)

type adminSummary struct {
	totalAgents     int
	activeAgents    int
	runningAgents   int
	planningAgents  int
	executingAgents int
	awaitingAgents  int
	crashedAgents   int
	totalTokens     int
	totalCacheReads int
	totalDuration   int64
	totalToolCalls  int
	totalFilesRead  int
	totalFilesWrite int
	totalLines      int
	totalRestarts   int
}

func (m Model) renderAdmin() string {
	var b strings.Builder

	summary := summarizeAdmin(m.agents)
	summaryBlock := m.renderAdminSummary(summary)

	b.WriteString(summaryBlock)
	b.WriteString("\n\n")

	b.WriteString(tui.StyleAccent.Render("Agent States"))
	b.WriteString("\n")

	b.WriteString(m.renderAdminAgentStates())
	b.WriteString("\n\n")

	b.WriteString(tui.StyleAccent.Render("Worktree States"))
	b.WriteString("\n")
	b.WriteString(m.renderAdminWorktreeStates())
	b.WriteString("\n\n")

	b.WriteString(tui.StyleAccent.Render("Recent Activity"))
	b.WriteString("\n")
	b.WriteString(m.renderAdminRecentActivity())
	b.WriteString("\n\n")

	b.WriteString(tui.StyleAccent.Render("Queues"))
	b.WriteString("\n")
	b.WriteString(m.renderAdminQueueStates())

	return b.String()
}

func (m Model) renderAdminSummary(summary adminSummary) string {
	gap := 2
	cols := 3
	// Account for left margin (3 chars) and some breathing room
	usableWidth := m.width - 6
	cardWidth := (usableWidth - gap*(cols-1)) / cols
	if cardWidth < 30 {
		cols = 2
		cardWidth = (usableWidth - gap) / 2
	}
	if cardWidth < 30 {
		cols = 1
		cardWidth = usableWidth
	}
	if cardWidth < 1 {
		cardWidth = 1
	}

	// Live update indicator
	updateIndicator := "◌"
	if time.Since(m.lastUpdate) < 2*time.Second {
		updateIndicator = "●"
	}
	updated := "n/a"
	if !m.lastUpdate.IsZero() {
		updated = fmt.Sprintf("%s ago", formatDuration(time.Since(m.lastUpdate)))
	}

	daemonStatus := tui.StyleDanger.Render("offline")
	if m.client != nil && m.client.Connected() {
		daemonStatus = tui.StyleSuccess.Render("online")
	}

	attention := summary.awaitingAgents + summary.crashedAgents
	attentionStyle := tui.StyleMuted
	if attention > 0 {
		attentionStyle = tui.StyleWarning
		if summary.crashedAgents > 0 {
			attentionStyle = tui.StyleDanger
		}
	}

	// Token usage with progress bar (assume 100K max for visualization)
	tokenMax := 100000
	tokenPct := float64(summary.totalTokens) / float64(tokenMax) * 100
	if tokenPct > 100 {
		tokenPct = 100
	}
	tokenBar := components.NewProgressBar(int(tokenPct), 100).
		WithWidth(10).
		WithColor(tui.ColorAccent)

	// Cache reads - show actual count (these are prompt cache hits from Claude)
	cacheReadsStr := formatCompactNumber(summary.totalCacheReads)

	cards := []string{
		adminCard("System", []string{
			adminMetricLine("daemon", daemonStatus),
			adminMetricLine("workflow", m.renderWorkflowStatus()),
			adminMetricLine("updated", tui.StyleMuted.Render(updateIndicator)+" "+updated),
		}, cardWidth),
		adminCard("Agents", []string{
			adminMetricLine("active", tui.StyleAccent.Render(fmt.Sprintf("%d/%d", summary.activeAgents, summary.totalAgents))),
			adminMetricLine("attention", attentionStyle.Render(fmt.Sprintf("%d", attention))),
			adminMetricLine("restarts", fmt.Sprintf("%d total", summary.totalRestarts)),
		}, cardWidth),
		adminCard("Tokens", []string{
			fmt.Sprintf("used    %s %s", tokenBar.Render(), tui.StyleMuted.Render(formatCompactNumber(summary.totalTokens))),
			adminMetricLine("cached", tui.StyleSuccess.Render(cacheReadsStr)+" reads"),
			adminMetricLine("tools", fmt.Sprintf("%d calls", summary.totalToolCalls)),
		}, cardWidth),
	}

	var rows []string
	for i := 0; i < len(cards); i += cols {
		end := min(i+cols, len(cards))
		rows = append(rows, joinCards(cards[i:end], gap))
	}

	return strings.Join(rows, "\n")
}

type adminStateRow struct {
	label string
	style lipgloss.Style
	count int
	last  time.Time
}

type adminCountRow struct {
	label  string
	style  lipgloss.Style
	count  int
	detail string
}

func (m Model) renderAdminAgentStates() string {
	table := layout.NewTable([]layout.Column{
		{Header: "STATE", MinWidth: 10, MaxWidth: 0, Flex: 1},
		{Header: "COUNT", MinWidth: 5, MaxWidth: 6, Flex: 0},
		{Header: "LAST", MinWidth: 8, MaxWidth: 12, Flex: 0},
	})
	table.SetWidth(m.width)

	rows := adminAgentStateRows(m.agents)

	var b strings.Builder
	b.WriteString(table.RenderHeader())
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No agents yet."))
		return b.String()
	}

	for _, row := range rows {
		last := "-"
		if !row.last.IsZero() {
			last = fmt.Sprintf("%s ago", formatDuration(time.Since(row.last)))
		}
		values := []string{
			row.style.Render(strings.ToUpper(row.label)),
			fmt.Sprintf("%d", row.count),
			tui.StyleMuted.Render(last),
		}
		b.WriteString(table.RenderRowStyled(values, false))
		b.WriteString("\n")
	}

	return b.String()
}

func adminAgentStateRows(agents []*control.AgentInfo) []adminStateRow {
	order := []string{"running", "planning", "executing", "awaiting", "completed", "crashed", "terminated", "stopped"}
	rows := make(map[string]*adminStateRow, len(order))
	for _, state := range order {
		rows[state] = &adminStateRow{label: state, style: statusStyleForAgent(state)}
	}

	for _, agent := range agents {
		state := agent.Status
		row := rows[state]
		if row == nil {
			row = &adminStateRow{label: state, style: statusStyleForAgent(state)}
			rows[state] = row
			order = append(order, state)
		}
		row.count++
		last := adminSortTime(agent)
		if last.After(row.last) {
			row.last = last
		}
	}

	var result []adminStateRow
	for _, state := range order {
		row := rows[state]
		if row != nil && row.count > 0 {
			result = append(result, *row)
		}
	}
	return result
}

func (m Model) renderAdminWorktreeStates() string {
	table := layout.NewTable([]layout.Column{
		{Header: "STATE", MinWidth: 12, MaxWidth: 0, Flex: 1},
		{Header: "COUNT", MinWidth: 5, MaxWidth: 6, Flex: 0},
	})
	table.SetWidth(m.width)

	rows := m.adminWorktreeStateRows()
	return renderAdminCountTable(table, rows, "   No worktrees yet.")
}

func (m Model) renderAdminRecentActivity() string {
	table := layout.NewTable([]layout.Column{
		{Header: "AGENT", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "STATE", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "TYPE", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "WORKTREE", MinWidth: 10, MaxWidth: 0, Flex: 1},
		{Header: "LAST", MinWidth: 8, MaxWidth: 12, Flex: 0},
		{Header: "ACTIVITY", MinWidth: 12, MaxWidth: 0, Flex: 2},
	})
	table.SetWidth(m.width)

	rows := adminRecentActivityRows(m.agents)

	var b strings.Builder
	b.WriteString(table.RenderHeader())
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No recent activity yet."))
		return b.String()
	}

	for _, row := range rows {
		last := "-"
		if !row.last.IsZero() {
			last = fmt.Sprintf("%s ago", formatDuration(time.Since(row.last)))
		}
		values := []string{
			shortID(row.agentID, 8),
			tui.StatusStyle(row.status).Render(strings.ToUpper(row.status)),
			agentTypeLabel(row.archetype),
			row.worktree,
			tui.StyleMuted.Render(last),
			row.activity,
		}
		b.WriteString(table.RenderRowStyled(values, false))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) adminWorktreeStateRows() []adminCountRow {
	total := len(m.worktrees)
	if total == 0 {
		return nil
	}

	merged := 0
	stale := 0
	dirty := 0
	withAgents := 0
	for _, wt := range m.worktrees {
		switch wt.WTStatus {
		case "merged":
			merged++
		case "stale":
			stale++
		}
		if isDirtyGitStatus(wt.Status) {
			dirty++
		}
		if m.worktreeAgentCount(wt) > 0 {
			withAgents++
		}
	}

	active := total - merged - stale
	if active < 0 {
		active = 0
	}
	clean := total - dirty
	if clean < 0 {
		clean = 0
	}

	return []adminCountRow{
		{label: "total", style: tui.StyleAccent, count: total},
		{label: "active", style: tui.StyleSuccess, count: active},
		{label: "with agents", style: tui.StyleInfo, count: withAgents},
		{label: "dirty", style: tui.StyleWarning, count: dirty},
		{label: "clean", style: tui.StyleMuted, count: clean},
		{label: "merged", style: tui.StyleNeutral, count: merged},
		{label: "stale", style: tui.StyleWarning, count: stale},
	}
}

func (m Model) renderAdminQueueStates() string {
	table := layout.NewTable([]layout.Column{
		{Header: "ITEM", MinWidth: 10, MaxWidth: 0, Flex: 1},
		{Header: "COUNT", MinWidth: 5, MaxWidth: 6, Flex: 0},
		{Header: "DETAIL", MinWidth: 10, MaxWidth: 0, Flex: 1},
	})
	table.SetWidth(m.width)

	rows := m.adminQueueRows()
	return renderAdminCountTable(table, rows, "   No queue data yet.")
}

func (m Model) adminQueueRows() []adminCountRow {
	jobsTotal := 0
	jobsActive := 0
	for _, j := range m.jobs {
		if j.Type == "question" {
			continue
		}
		jobsTotal++
		if j.Status != "completed" {
			jobsActive++
		}
	}

	questions := m.questions()
	answered := 0
	for _, q := range questions {
		if q.Answer != "" || q.Status == "completed" {
			answered++
		}
	}
	questionTotal := len(questions)
	questionPending := questionTotal - answered

	pending, inProgress, completed := countTaskStatuses(m.claudeTasks)

	openNotes := 0
	for _, note := range m.notes {
		if !note.Done {
			openNotes++
		}
	}

	return []adminCountRow{
		{
			label:  "jobs",
			style:  tui.StyleAccent,
			count:  jobsTotal,
			detail: fmt.Sprintf("%d active", jobsActive),
		},
		{
			label:  "questions",
			style:  tui.StyleInfo,
			count:  questionTotal,
			detail: fmt.Sprintf("%d pending", questionPending),
		},
		{
			label:  "tasks",
			style:  tui.StyleAccent,
			count:  len(m.claudeTasks),
			detail: fmt.Sprintf("%d pending, %d in progress, %d done", pending, inProgress, completed),
		},
		{
			label:  "notes",
			style:  tui.StyleMuted,
			count:  openNotes,
			detail: fmt.Sprintf("%d total", len(m.notes)),
		},
	}
}

type adminRecentRow struct {
	status    string
	archetype string
	project   string
	worktree  string
	last      time.Time
	activity  string
	agentID   string
}

func adminRecentActivityRows(agents []*control.AgentInfo) []adminRecentRow {
	if len(agents) == 0 {
		return nil
	}

	rows := make([]adminRecentRow, 0, len(agents))
	for _, agent := range agents {
		project := agent.ProjectName
		if project == "" {
			project = agent.Project
		}
		if project == "" {
			project = "-"
		}

		activity := adminActivitySummary(agent)
		if activity == "" {
			activity = "-"
		}

		worktree := "-"
		if agent.WorktreePath != "" {
			worktree = filepath.Base(agent.WorktreePath)
		}

		rows = append(rows, adminRecentRow{
			status:    agent.Status,
			archetype: agent.Archetype,
			project:   project,
			worktree:  worktree,
			last:      adminSortTime(agent),
			activity:  activity,
			agentID:   agent.ID,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].last.After(rows[j].last)
	})

	limit := 8
	if len(rows) < limit {
		limit = len(rows)
	}

	return rows[:limit]
}

func adminActivitySummary(agent *control.AgentInfo) string {
	if agent.LastActivity != "" {
		return agent.LastActivity
	}
	if agent.LastEventType != "" {
		return agent.LastEventType
	}
	if agent.Archetype == "planner" && agent.Status == "completed" && agent.PlanStatus != "" {
		return "plan " + agent.PlanStatus
	}
	if agent.Status != "" {
		return agent.Status
	}
	return ""
}

func renderAdminCountTable(table *layout.Table, rows []adminCountRow, emptyMessage string) string {
	var b strings.Builder
	b.WriteString(table.RenderHeader())
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(tui.StyleEmptyState.Render(emptyMessage))
		return b.String()
	}

	for _, row := range rows {
		label := strings.ToUpper(row.label)
		detail := row.detail
		if detail != "" {
			detail = tui.StyleMuted.Render(detail)
		}
		values := []string{
			row.style.Render(label),
			fmt.Sprintf("%d", row.count),
		}
		if len(table.Columns) > 2 {
			values = append(values, detail)
		}
		b.WriteString(table.RenderRowStyled(values, false))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderAdminAgentRow(agent *control.AgentInfo, table *layout.Table, selected bool) string {
	tokens := tui.StyleMuted.Render("-")
	duration := tui.StyleMuted.Render("-")
	if agent.Metrics != nil {
		tokens = formatCompactNumber(agent.Metrics.TotalTokens)
		duration = formatDurationMs(agent.Metrics.DurationMs)
	}

	project := agent.ProjectName
	if project == "" {
		project = agent.Project
	}
	if project == "" {
		project = "-"
	}

	activity := m.formatAgentActivity(agent)
	if activity == "" {
		activity = "-"
	}

	worktree := truncatePath(agent.WorktreePath, 26)
	if worktree == "" {
		worktree = "-"
	}

	statusText := strings.ToUpper(agent.Status)
	if statusText == "" {
		statusText = "UNKNOWN"
	}
	status := tui.StatusStyle(agent.Status).Render(statusText)

	restarts := fmt.Sprintf("%d", agent.RestartCount)

	values := []string{
		shortID(agent.ID, 8),
		project,
		status,
		activity,
		worktree,
		tokens,
		duration,
		restarts,
	}

	return table.RenderRowStyled(values, selected)
}

func summarizeAdmin(agents []*control.AgentInfo) adminSummary {
	summary := adminSummary{
		totalAgents: len(agents),
	}

	for _, a := range agents {
		switch a.Status {
		case "running":
			summary.runningAgents++
			summary.activeAgents++
		case "planning":
			summary.planningAgents++
			summary.activeAgents++
		case "executing":
			summary.executingAgents++
			summary.activeAgents++
		case "awaiting":
			summary.awaitingAgents++
		case "crashed":
			summary.crashedAgents++
		}

		summary.totalRestarts += a.RestartCount

		if a.Metrics != nil {
			summary.totalTokens += a.Metrics.TotalTokens
			summary.totalCacheReads += a.Metrics.CacheReads
			summary.totalDuration += a.Metrics.DurationMs
			summary.totalToolCalls += a.Metrics.ToolUseCount
			summary.totalFilesRead += a.Metrics.FilesRead
			summary.totalFilesWrite += a.Metrics.FilesWritten
			summary.totalLines += a.Metrics.LinesChanged
		}
	}

	return summary
}

func adminMetricLine(label, value string) string {
	return fmt.Sprintf("%s %s", tui.StyleMuted.Render(label), value)
}

func adminCard(title string, lines []string, width int) string {
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorAccentDim).
		Padding(0, 1)

	content := append([]string{tui.StyleHeader.Render(title)}, lines...)
	return cardStyle.Width(width).Render(strings.Join(content, "\n"))
}

func joinCards(cards []string, gap int) string {
	if len(cards) == 0 {
		return ""
	}
	if len(cards) == 1 {
		return cards[0]
	}

	spacer := lipgloss.NewStyle().Width(gap).Render("")
	blocks := make([]string, 0, len(cards)*2-1)
	for i, card := range cards {
		if i > 0 {
			blocks = append(blocks, spacer)
		}
		blocks = append(blocks, card)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}

func adminVisibleColumns(width int) []bool {
	if width < 85 {
		return []bool{true, true, true, true, false, false, false, false}
	}
	if width < 100 {
		return []bool{true, true, true, true, true, false, false, false}
	}
	if width < 115 {
		return []bool{true, true, true, true, true, true, false, false}
	}
	if width < 130 {
		return []bool{true, true, true, true, true, true, true, false}
	}
	return []bool{true, true, true, true, true, true, true, true}
}

func adminSortTime(agent *control.AgentInfo) time.Time {
	if agent.LastActivityTime != "" {
		if t := parseCreatedAt(agent.LastActivityTime); !t.IsZero() {
			return t
		}
	}
	if agent.CreatedAt != "" {
		if t := parseCreatedAt(agent.CreatedAt); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func formatDurationMs(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Preserve the end of the path
	return "..." + path[len(path)-maxLen+3:]
}
