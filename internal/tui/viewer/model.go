// Package viewer provides a live agent output viewer.
package viewer

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
)

// Model represents the live viewer for an agent's output.
type Model struct {
	agentID   string
	agentInfo *control.AgentInfo
	events    []EventLine
	viewport  viewport.Model
	width     int
	height    int
	client    *control.Client
	ready     bool
	autoScroll bool
}

// EventLine represents a formatted event for display.
type EventLine struct {
	Time    time.Time
	Type    string
	Content string
}

type (
	tickMsg      time.Time
	eventsMsg    []EventLine
	agentInfoMsg *control.AgentInfo
	errMsg       error
)

// New creates a new viewer for the given agent.
func New(client *control.Client, agentID string) Model {
	return Model{
		client:     client,
		agentID:    agentID,
		events:     make([]EventLine, 0),
		autoScroll: true,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchAgentInfo,
		m.fetchEvents,
		m.tick(),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "g":
			m.viewport.GotoTop()
		case "G":
			m.viewport.GotoBottom()
		case "f":
			m.autoScroll = !m.autoScroll
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-6)
			m.viewport.YPosition = 4
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 6
		}

	case tickMsg:
		cmds = append(cmds, m.fetchEvents, m.tick())

	case eventsMsg:
		m.events = append(m.events, msg...)
		m.updateContent()
		if m.autoScroll {
			m.viewport.GotoBottom()
		}

	case agentInfoMsg:
		m.agentInfo = msg
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	var b strings.Builder

	// Header
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(tui.StyleMuted.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// Viewport with events
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Footer
	b.WriteString(tui.StyleMuted.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	icon := tui.AgentIcons["running"]
	status := "unknown"
	worktree := m.agentID

	if m.agentInfo != nil {
		status = m.agentInfo.Status
		worktree = m.agentInfo.WorktreePath
		if len(worktree) > 50 {
			worktree = "..." + worktree[len(worktree)-47:]
		}
	}

	iconStyle := tui.StatusStyle(status)
	return fmt.Sprintf("%s  %s  %s",
		iconStyle.Render(icon),
		tui.StyleTitle.Render("Agent Viewer"),
		tui.StyleMuted.Render(worktree))
}

func (m Model) renderFooter() string {
	scrollMode := "auto-scroll: ON"
	if !m.autoScroll {
		scrollMode = "auto-scroll: OFF"
	}

	help := "[q] quit  [g/G] top/bottom  [f] toggle follow  [↑↓] scroll"
	return tui.StyleMuted.Render(fmt.Sprintf("%s  │  %s", help, scrollMode))
}

func (m *Model) updateContent() {
	var lines []string

	for _, e := range m.events {
		timeStr := e.Time.Format("15:04:05")
		typeStyle := m.styleForEventType(e.Type)

		line := fmt.Sprintf("%s %s %s",
			tui.StyleMuted.Render(timeStr),
			typeStyle.Render(fmt.Sprintf("%-12s", e.Type)),
			e.Content)
		lines = append(lines, line)
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
}

func (m Model) styleForEventType(eventType string) lipgloss.Style {
	switch eventType {
	case "thinking":
		return lipgloss.NewStyle().Foreground(tui.ColorInfo)
	case "tool_use":
		return lipgloss.NewStyle().Foreground(tui.ColorAccent)
	case "tool_result":
		return lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
	case "text":
		return lipgloss.NewStyle().Foreground(tui.ColorFg)
	case "error":
		return lipgloss.NewStyle().Foreground(tui.ColorDanger)
	case "result":
		return lipgloss.NewStyle().Foreground(tui.ColorSuccess)
	default:
		return lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
	}
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) fetchAgentInfo() tea.Msg {
	if m.client == nil {
		return nil
	}
	info, err := m.client.GetAgent(m.agentID)
	if err != nil {
		return errMsg(err)
	}
	return agentInfoMsg(info)
}

func (m Model) fetchEvents() tea.Msg {
	// TODO: Implement event fetching from daemon
	// For now, return empty - will be filled in when we add event streaming
	return eventsMsg(nil)
}
