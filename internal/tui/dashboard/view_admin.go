package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/layout"
)

func (m Model) renderAdmin() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	// Summary Stats
	totalAgents := len(m.agents)
	activeAgents := 0
	totalTokens := 0
	totalCacheReads := 0
	totalDuration := int64(0)

	for _, a := range m.agents {
		if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
			activeAgents++
		}
		if a.Metrics != nil {
			totalTokens += a.Metrics.TotalTokens
			totalCacheReads += a.Metrics.CacheReads
			totalDuration += a.Metrics.DurationMs
		}
	}

	b.WriteString(tui.StyleAccent.Render("System Status"))
	b.WriteString("\n")

	stats := []string{
		fmt.Sprintf("Agents: %d (%d active)", totalAgents, activeAgents),
		fmt.Sprintf("Total Tokens: %s", formatCompactNumber(totalTokens)),
		fmt.Sprintf("Cache Reads: %d", totalCacheReads),
		fmt.Sprintf("Total Compute: %s", formatDurationMs(totalDuration)),
	}
	b.WriteString(strings.Join(stats, "  │  "))
	b.WriteString("\n\n")

	b.WriteString(tui.StyleAccent.Render("Agent Performance & Context"))
	b.WriteString("\n")

	// Agent Table
	table := layout.NewTable([]layout.Column{
		{Header: "AGENT", MinWidth: 15, MaxWidth: 20, Flex: 1},
		{Header: "WORKTREE", MinWidth: 20, MaxWidth: 40, Flex: 2},
		{Header: "TOKENS", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "CACHE", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "DUR", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "STATUS", MinWidth: 10, MaxWidth: 12, Flex: 0},
	})
	table.SetWidth(m.width)

	b.WriteString(table.RenderHeader())
	b.WriteString("\n")

	// Filter and sort agents (most recent first)
	agents := make([]*control.AgentInfo, len(m.agents))
	copy(agents, m.agents)
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].CreatedAt > agents[j].CreatedAt // Descending
	})

	// Calculate scroll
	// Subtract header(3) + summary(3) + footer(1)
	availableRows := max(contentHeight-7, 1)

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

func (m Model) renderAdminAgentRow(agent *control.AgentInfo, table *layout.Table, selected bool) string {
	tokens := "0"
	cache := "0"
	duration := "0s"

	if agent.Metrics != nil {
		tokens = formatCompactNumber(agent.Metrics.TotalTokens)
		cache = fmt.Sprintf("%d", agent.Metrics.CacheReads)
		duration = formatDurationMs(agent.Metrics.DurationMs)
	}

	statusStyle := tui.StatusStyle(agent.Status)
	status := statusStyle.Render(agent.Status)

	values := []string{
		shortID(agent.ID, 8),
		truncatePath(agent.WorktreePath, 30),
		tokens,
		cache,
		duration,
		status,
	}

	return table.RenderRow(values, selected)
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
