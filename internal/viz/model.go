// Package viz provides the data flow visualization TUI.
package viz

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
)

// View represents the current visualization mode.
type View int

const (
	ViewTimeline View = iota // Chronological event list
	ViewFlow                  // Network diagram with data flow
	ViewDetail                // Expanded event inspector
)

// Model is the main visualization model.
type Model struct {
	// Client connection
	client *control.Client
	filter control.SubscribeStreamRequest

	// Event buffer (ring buffer, newest at end)
	events     []*control.StreamEvent
	maxEvents  int
	eventIndex int // Index of oldest event in ring buffer

	// View state
	view           View
	width          int
	height         int
	selected       int  // Selected event index in timeline
	scrollOffset   int  // Scroll position
	paused         bool // Pause event collection
	showTimestamps bool
	showPayloads   bool

	// Detail view state
	detailEvent *control.StreamEvent

	// Flow diagram state
	flowNodes   []FlowNode
	flowEdges   []FlowEdge
	activeEdges map[string]time.Time // Edge ID -> last activity time

	// Stats
	activeAgents    int
	totalEvents     int
	eventsPerSecond float64
	lastEventTime   time.Time
	eventCounts     map[control.StreamEventType]int

	// UI components
	spinner spinner.Model
	err     error
}

// FlowNode represents a component in the flow diagram.
type FlowNode struct {
	ID     string
	Label  string
	Type   control.StreamSource
	X, Y   int
	Active bool
}

// FlowEdge represents a data flow connection.
type FlowEdge struct {
	ID       string
	From, To string
	Label    string
	Active   bool
}

// NewModel creates a new visualization model.
func NewModel(client *control.Client, filter control.SubscribeStreamRequest, activeAgents int) *Model {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = tui.StyleAccent

	// Initialize flow diagram nodes
	nodes := []FlowNode{
		{ID: "store", Label: "SQLite", Type: control.StreamSourceStore, X: 0, Y: 1},
		{ID: "daemon", Label: "athenad", Type: control.StreamSourceDaemon, X: 1, Y: 1},
		{ID: "agent", Label: "Claude", Type: control.StreamSourceAgent, X: 2, Y: 1},
		{ID: "api", Label: "API", Type: control.StreamSourceAPI, X: 1, Y: 0},
	}

	// Initialize flow diagram edges
	edges := []FlowEdge{
		{ID: "store-daemon", From: "store", To: "daemon", Label: "query"},
		{ID: "daemon-store", From: "daemon", To: "store", Label: "persist"},
		{ID: "daemon-agent", From: "daemon", To: "agent", Label: "spawn"},
		{ID: "agent-daemon", From: "agent", To: "daemon", Label: "events"},
		{ID: "api-daemon", From: "api", To: "daemon", Label: "RPC"},
		{ID: "daemon-api", From: "daemon", To: "api", Label: "response"},
	}

	return &Model{
		client:         client,
		filter:         filter,
		events:         make([]*control.StreamEvent, 0, 1000),
		maxEvents:      1000,
		activeAgents:   activeAgents,
		eventCounts:    make(map[control.StreamEventType]int),
		activeEdges:    make(map[string]time.Time),
		spinner:        s,
		flowNodes:      nodes,
		flowEdges:      edges,
		showTimestamps: true,
	}
}

// Messages

type tickMsg time.Time

type streamEventMsg *control.StreamEvent

type errMsg error

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
		m.listenForEvents(),
	)
}

// tickCmd returns a command that ticks every 100ms.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// listenForEvents returns a command that listens for stream events.
func (m *Model) listenForEvents() tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-m.client.StreamEvents():
			return streamEventMsg(event)
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	}
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Update events per second calculation
		if !m.lastEventTime.IsZero() {
			elapsed := time.Since(m.lastEventTime).Seconds()
			if elapsed > 0 && elapsed < 5 {
				m.eventsPerSecond = float64(m.totalEvents) / elapsed
			}
		}

		// Decay active edges
		now := time.Now()
		for id, t := range m.activeEdges {
			if now.Sub(t) > 2*time.Second {
				delete(m.activeEdges, id)
			}
		}

		cmds = append(cmds, tickCmd())

	case streamEventMsg:
		if msg != nil && !m.paused {
			m.addEvent(msg)
		}
		cmds = append(cmds, m.listenForEvents())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case errMsg:
		m.err = msg
	}

	return m, tea.Batch(cmds...)
}

// handleKey processes keyboard input.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "1":
		m.view = ViewTimeline
	case "2":
		m.view = ViewFlow
	case "3":
		if m.view == ViewTimeline && len(m.events) > 0 && m.selected < len(m.events) {
			m.view = ViewDetail
			m.detailEvent = m.events[m.selected]
		}

	case "up", "k":
		if m.view == ViewTimeline && m.selected > 0 {
			m.selected--
			m.ensureVisible()
		}
	case "down", "j":
		if m.view == ViewTimeline && m.selected < len(m.events)-1 {
			m.selected++
			m.ensureVisible()
		}
	case "pgup":
		if m.view == ViewTimeline {
			m.selected -= m.visibleLines()
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
		}
	case "pgdown":
		if m.view == ViewTimeline {
			m.selected += m.visibleLines()
			if m.selected >= len(m.events) {
				m.selected = len(m.events) - 1
			}
			if m.selected < 0 {
				m.selected = 0
			}
			m.ensureVisible()
		}
	case "home", "g":
		if m.view == ViewTimeline {
			m.selected = 0
			m.scrollOffset = 0
		}
	case "end", "G":
		if m.view == ViewTimeline && len(m.events) > 0 {
			m.selected = len(m.events) - 1
			m.ensureVisible()
		}

	case "enter":
		if m.view == ViewTimeline && len(m.events) > 0 && m.selected < len(m.events) {
			m.view = ViewDetail
			m.detailEvent = m.events[m.selected]
		}

	case "esc", "backspace":
		if m.view == ViewDetail {
			m.view = ViewTimeline
			m.detailEvent = nil
		}

	case " ":
		m.paused = !m.paused

	case "t":
		m.showTimestamps = !m.showTimestamps

	case "p":
		m.showPayloads = !m.showPayloads

	case "c":
		// Clear events
		m.events = m.events[:0]
		m.selected = 0
		m.scrollOffset = 0
		m.totalEvents = 0
		m.eventCounts = make(map[control.StreamEventType]int)
	}

	return m, nil
}

// addEvent adds a new event to the buffer.
func (m *Model) addEvent(event *control.StreamEvent) {
	m.totalEvents++
	m.lastEventTime = time.Now()
	m.eventCounts[event.Type]++

	// Update active edges based on event source
	m.updateFlowActivity(event)

	// Add to ring buffer
	if len(m.events) < m.maxEvents {
		m.events = append(m.events, event)
	} else {
		// Overwrite oldest
		m.events[m.eventIndex] = event
		m.eventIndex = (m.eventIndex + 1) % m.maxEvents
	}

	// Auto-scroll to bottom if at bottom
	if m.selected >= len(m.events)-2 {
		m.selected = len(m.events) - 1
		m.ensureVisible()
	}
}

// updateFlowActivity marks edges as active based on the event.
func (m *Model) updateFlowActivity(event *control.StreamEvent) {
	now := time.Now()

	switch event.Source {
	case control.StreamSourceAgent:
		m.activeEdges["agent-daemon"] = now
	case control.StreamSourceDaemon:
		if strings.HasPrefix(string(event.Type), "agent_") {
			m.activeEdges["daemon-agent"] = now
		}
	case control.StreamSourceStore:
		m.activeEdges["store-daemon"] = now
	case control.StreamSourceAPI:
		if event.Type == control.StreamEventAPICall {
			m.activeEdges["api-daemon"] = now
		} else if event.Type == control.StreamEventAPIResponse {
			m.activeEdges["daemon-api"] = now
		}
	}
}

// ensureVisible scrolls to keep the selected item visible.
func (m *Model) ensureVisible() {
	visible := m.visibleLines()
	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
	} else if m.selected >= m.scrollOffset+visible {
		m.scrollOffset = m.selected - visible + 1
	}
}

// visibleLines returns the number of lines visible in the timeline.
func (m *Model) visibleLines() int {
	// Header (4 lines) + footer (2 lines)
	return m.height - 6
}

// View renders the model.
func (m *Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Main content based on view
	switch m.view {
	case ViewTimeline:
		b.WriteString(m.renderTimeline())
	case ViewFlow:
		b.WriteString(m.renderFlow())
	case ViewDetail:
		b.WriteString(m.renderDetail())
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// renderHeader renders the header bar.
func (m *Model) renderHeader() string {
	// Title and view tabs
	title := tui.StyleLogo.Render("athena-viz")

	tabs := []string{
		m.tabStyle(ViewTimeline, "1:Timeline"),
		m.tabStyle(ViewFlow, "2:Flow"),
	}
	if m.detailEvent != nil {
		tabs = append(tabs, m.tabStyle(ViewDetail, "3:Detail"))
	}

	tabsStr := strings.Join(tabs, " ")

	// Stats
	statusIcon := m.spinner.View()
	if m.paused {
		statusIcon = tui.StyleWarning.Render("||")
	}

	stats := fmt.Sprintf("%s %d events | %d agents | %.1f/s",
		statusIcon,
		len(m.events),
		m.activeAgents,
		m.eventsPerSecond,
	)

	// Layout: title | tabs | stats
	gap := m.width - len(title) - len(tabsStr) - len(stats) - 10
	if gap < 0 {
		gap = 1
	}

	return fmt.Sprintf("%s  %s%s%s",
		title,
		tabsStr,
		strings.Repeat(" ", gap),
		tui.StyleMuted.Render(stats),
	)
}

// tabStyle returns styled tab text.
func (m *Model) tabStyle(v View, label string) string {
	if m.view == v {
		return tui.StyleTabActive.Render("[" + label + "]")
	}
	return tui.StyleTabInactive.Render(" " + label + " ")
}

// renderFooter renders the help bar.
func (m *Model) renderFooter() string {
	var help string
	switch m.view {
	case ViewTimeline:
		help = "j/k:navigate  enter:detail  space:pause  t:timestamps  p:payloads  c:clear  q:quit"
	case ViewFlow:
		help = "1:timeline  space:pause  q:quit"
	case ViewDetail:
		help = "esc:back  1:timeline  2:flow  q:quit"
	}

	// Filter indicator
	var filter string
	if m.filter.AgentID != "" {
		filter += fmt.Sprintf(" agent:%s", m.filter.AgentID)
	}
	if m.filter.WorktreePath != "" {
		filter += fmt.Sprintf(" wt:%s", m.filter.WorktreePath)
	}
	if filter != "" {
		filter = tui.StyleInfo.Render(" [filter:" + strings.TrimSpace(filter) + "]")
	}

	return tui.StyleHelp.Render(help) + filter
}
