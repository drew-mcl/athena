package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
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

	contentHeight := layout.ContentHeight(m.height)
	summary := summarizeAdmin(m.agents)
	summaryBlock := m.renderAdminSummary(summary)
	summaryHeight := lipgloss.Height(summaryBlock)

	b.WriteString(summaryBlock)
	b.WriteString("\n\n")

	b.WriteString(tui.StyleAccent.Render("Agent Activity"))
	b.WriteString("\n")

	table := layout.NewTable([]layout.Column{
		{Header: "AGENT", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "PROJECT", MinWidth: 10, MaxWidth: 16, Flex: 1},
		{Header: "STATUS", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "ACTIVITY", MinWidth: 18, MaxWidth: 40, Flex: 2},
		{Header: "WORKTREE", MinWidth: 14, MaxWidth: 28, Flex: 1},
		{Header: "TOKENS", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "DUR", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "RST", MinWidth: 3, MaxWidth: 5, Flex: 0},
	})
	table.SetWidth(m.width)
	table.SetVisibleColumns(adminVisibleColumns(m.width))

	b.WriteString(table.RenderHeader())
	b.WriteString("\n")

	agents := make([]*control.AgentInfo, len(m.agents))
	copy(agents, m.agents)
	sort.Slice(agents, func(i, j int) bool {
		return adminSortTime(agents[i]).After(adminSortTime(agents[j]))
	})

	if len(agents) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No agents yet."))
		return b.String()
	}

	overhead := summaryHeight + 3
	availableRows := max(contentHeight-overhead, 1)

	scroll := layout.CalculateScrollWindow(len(agents), m.selected, availableRows)

	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	end := min(scroll.Offset+scroll.VisibleRows, len(agents))

	for i := scroll.Offset; i < end; i++ {
		agent := agents[i]
		row := m.renderAdminAgentRow(agent, table, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	if scroll.HasMore {
		remaining := len(agents) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderAdminSummary(summary adminSummary) string {
	gap := 2
	cols := 3
	cardWidth := (m.width - gap*(cols-1)) / cols
	if cardWidth < 24 {
		cols = 2
		cardWidth = (m.width - gap) / 2
	}
	if cardWidth < 24 {
		cols = 1
		cardWidth = m.width
	}
	if cardWidth < 1 {
		cardWidth = 1
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

	cards := []string{
		adminCard("Health", []string{
			adminMetricLine("daemon", daemonStatus),
			adminMetricLine("workflow", m.renderWorkflowStatus()),
			adminMetricLine("updated", updated),
		}, cardWidth),
		adminCard("Agents", []string{
			adminMetricLine("active", tui.StyleAccent.Render(fmt.Sprintf("%d/%d", summary.activeAgents, summary.totalAgents))),
			adminMetricLine("attention", attentionStyle.Render(fmt.Sprintf("%d", attention))),
			adminMetricLine("restarts", fmt.Sprintf("%d", summary.totalRestarts)),
		}, cardWidth),
		adminCard("Usage", []string{
			adminMetricLine("tokens", tui.StyleAccent.Render(formatCompactNumber(summary.totalTokens))),
			adminMetricLine("cache", fmt.Sprintf("%d", summary.totalCacheReads)),
			adminMetricLine("compute", formatDurationMs(summary.totalDuration)),
		}, cardWidth),
		adminCard("Ops", []string{
			adminMetricLine("tools", fmt.Sprintf("%d", summary.totalToolCalls)),
			adminMetricLine("files", fmt.Sprintf("%dR/%dW", summary.totalFilesRead, summary.totalFilesWrite)),
			adminMetricLine("changes", fmt.Sprintf("%d", summary.totalLines)),
		}, cardWidth),
	}

	var rows []string
	for i := 0; i < len(cards); i += cols {
		end := min(i+cols, len(cards))
		rows = append(rows, joinCards(cards[i:end], gap))
	}

	return strings.Join(rows, "\n")
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

	activity := formatActivity(agent)
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
