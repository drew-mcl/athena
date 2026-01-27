package dashboard

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/layout"
)

const (
	agentColState = iota
	agentColType
	agentColProject
	agentColWorktree
	agentColStatus
	agentColActivity
	agentColTokens
	agentColTools
	agentColAge
)

type agentTableConfig struct {
	showProject  bool
	showWorktree bool
	showActivity bool
}

func (m Model) newAgentTable(config agentTableConfig) *layout.Table {
	columns := agentTableColumns()
	table := layout.NewTable(columns)
	table.SetWidth(m.width)
	table.SetVisibleColumns(agentTableVisibleColumns(columns, m.width, config))
	return table
}

func agentTableColumns() []layout.Column {
	return []layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "TYPE", MinWidth: 8, MaxWidth: 12, Flex: 0},
		{Header: "PROJECT", MinWidth: 8, MaxWidth: 0, Flex: 1},
		{Header: "WORKTREE", MinWidth: 10, MaxWidth: 0, Flex: 1},
		{Header: "STATUS", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "ACTIVITY", MinWidth: 12, MaxWidth: 0, Flex: 2},
		{Header: "TOKENS", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "TOOLS", MinWidth: 5, MaxWidth: 6, Flex: 0},
		{Header: "AGE", MinWidth: 4, MaxWidth: 8, Flex: 0},
	}
}

func agentTableVisibleColumns(columns []layout.Column, width int, config agentTableConfig) []bool {
	visible := []bool{
		true, // ST
		true, // TYPE
		config.showProject,
		config.showWorktree,
		true, // STATUS
		config.showActivity,
		true, // TOKENS
		true, // TOOLS
		true, // AGE
	}

	hideOrder := []int{
		agentColAge,
		agentColTools,
		agentColTokens,
		agentColWorktree,
		agentColProject,
		agentColActivity,
	}

	for agentTableMinWidth(columns, visible) > width {
		hidden := false
		for _, idx := range hideOrder {
			if idx < len(visible) && visible[idx] {
				visible[idx] = false
				hidden = true
				break
			}
		}
		if !hidden {
			break
		}
	}

	return visible
}

func agentTableMinWidth(columns []layout.Column, visible []bool) int {
	total := 0
	visibleCount := 0
	for i, col := range columns {
		if i >= len(visible) || !visible[i] {
			continue
		}
		visibleCount++
		total += col.MinWidth
	}

	if visibleCount == 0 {
		return 0
	}

	total += layout.PaddingLeft + layout.PaddingRight
	total += layout.ColumnGap * (visibleCount - 1)

	return total
}

func (m Model) renderAgentTableRow(agent *control.AgentInfo, table *layout.Table, selected bool) string {
	return table.RenderRowStyled(m.agentRowValues(agent), selected)
}

func (m Model) agentRowValues(agent *control.AgentInfo) []string {
	statusIcon := agentStatusIndicator(agent)

	agentType := agent.Archetype
	if agentType == "" {
		agentType = "-"
	}
	agentType = agentTypeLabel(agentType)

	project := agent.ProjectName
	if project == "" {
		project = agent.Project
	}
	if project == "" {
		project = "-"
	}

	worktree := "-"
	if agent.WorktreePath != "" {
		worktree = filepath.Base(agent.WorktreePath)
	}

	statusText := agentStatusText(agent)
	status := agentStatusStyle(agent).Render(statusText)

	activity := m.formatAgentActivity(agent)
	if activity == "" {
		activity = tui.StyleMuted.Render("-")
	}

	tokens := "-"
	tools := "-"
	if agent.Metrics != nil {
		if agent.Metrics.TotalTokens > 0 {
			tokens = formatCompactNumber(agent.Metrics.TotalTokens)
		}
		if agent.Metrics.ToolUseCount > 0 {
			tools = fmt.Sprintf("%d", agent.Metrics.ToolUseCount)
		}
	}

	age := "-"
	if created := parseCreatedAt(agent.CreatedAt); !created.IsZero() {
		age = formatDuration(time.Since(created))
	}

	return []string{statusIcon, agentType, project, worktree, status, activity, tokens, tools, age}
}

func agentTypeLabel(archetype string) string {
	if archetype == "-" || archetype == "" {
		return "-"
	}

	switch archetype {
	case "planner":
		return tui.StyleInfo.Render(archetype)
	case "executor":
		return tui.StyleSuccess.Render(archetype)
	case "reviewer":
		return tui.StyleAccent.Render(archetype)
	default:
		return tui.StyleAccent.Render(archetype)
	}
}
