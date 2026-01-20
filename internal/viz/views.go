package viz

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
)

// renderTimeline renders the event timeline view.
func (m *Model) renderTimeline() string {
	if len(m.events) == 0 {
		return tui.StyleEmptyState.Render("\n  No events yet. Waiting for data flow...\n")
	}

	var b strings.Builder
	visible := m.visibleLines()

	// Column headers
	header := fmt.Sprintf("  %-12s %-8s %-20s %s",
		"TIME", "SOURCE", "TYPE", "DETAILS")
	b.WriteString(tui.StyleColumnHeader.Render(header))
	b.WriteString("\n")
	b.WriteString(tui.Divider(m.width))
	b.WriteString("\n")

	// Events
	start := m.scrollOffset
	end := start + visible
	if end > len(m.events) {
		end = len(m.events)
	}

	for i := start; i < end; i++ {
		event := m.events[i]
		line := m.renderEventLine(event, i == m.selected)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Padding for short lists
	for i := end - start; i < visible; i++ {
		b.WriteString("\n")
	}

	return b.String()
}

// renderEventLine renders a single event line in the timeline.
func (m *Model) renderEventLine(event *control.StreamEvent, selected bool) string {
	// Time
	var timeStr string
	if m.showTimestamps {
		timeStr = event.Timestamp.Format("15:04:05.000")
	} else {
		timeStr = event.Timestamp.Format("15:04:05")
	}

	// Source icon and color
	sourceIcon := sourceIcon(event.Source)
	sourceStyle := sourceStyle(event.Source)

	// Event type with icon and color
	typeIcon := eventIcon(event.Type)
	typeStyle := eventStyle(event.Type)
	typeName := truncate(string(event.Type), 18)

	// Details (truncated)
	details := m.eventDetails(event)
	maxDetails := m.width - 50
	if maxDetails < 10 {
		maxDetails = 10
	}
	details = truncate(details, maxDetails)

	// Build line
	indicator := "  "
	if selected {
		indicator = tui.StyleSelectedIndicator.Render("> ")
	}

	line := fmt.Sprintf("%s%s %s%s %s%-18s %s",
		indicator,
		tui.StyleMuted.Render(timeStr),
		sourceStyle.Render(sourceIcon),
		sourceStyle.Render(fmt.Sprintf("%-6s", event.Source)),
		typeStyle.Render(typeIcon),
		typeStyle.Render(typeName),
		tui.StyleSecondary.Render(details),
	)

	if selected {
		return tui.StyleSelected.Render(line)
	}
	return line
}

// eventDetails extracts a short description from the event payload.
func (m *Model) eventDetails(event *control.StreamEvent) string {
	if event.AgentID != "" {
		details := fmt.Sprintf("[%s]", truncate(event.AgentID, 12))
		if m.showPayloads && len(event.Payload) > 0 {
			var payload map[string]any
			if json.Unmarshal(event.Payload, &payload) == nil {
				if name, ok := payload["tool_name"].(string); ok {
					details += fmt.Sprintf(" %s", name)
				}
				if msg, ok := payload["content"].(string); ok {
					details += fmt.Sprintf(" %s", truncate(msg, 40))
				}
				if method, ok := payload["method"].(string); ok {
					details += fmt.Sprintf(" %s", method)
				}
			}
		}
		return details
	}

	if len(event.Payload) > 0 && m.showPayloads {
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) == nil {
			// Extract key fields
			if name, ok := payload["tool_name"].(string); ok {
				return fmt.Sprintf("tool: %s", name)
			}
			if method, ok := payload["method"].(string); ok {
				return fmt.Sprintf("method: %s", method)
			}
		}
	}

	return ""
}

// renderFlow renders the network flow diagram.
func (m *Model) renderFlow() string {
	var b strings.Builder

	// Title
	b.WriteString("\n")
	b.WriteString(tui.StyleTitle.Render("  Data Flow Diagram"))
	b.WriteString("\n\n")

	// ASCII art diagram with activity indicators
	// Layout:
	//              [API]
	//                |
	//  [SQLite] -- [athenad] -- [Claude]
	//

	apiActive := m.isNodeActive("api")
	daemonActive := m.isNodeActive("daemon")
	storeActive := m.isNodeActive("store")
	agentActive := m.isNodeActive("agent")

	// Row 1: API
	b.WriteString(strings.Repeat(" ", 20))
	b.WriteString(m.renderNode("API", apiActive))
	b.WriteString("\n")

	// Row 2: Vertical connection
	b.WriteString(strings.Repeat(" ", 24))
	edge1 := m.edgeActive("api-daemon") || m.edgeActive("daemon-api")
	if edge1 {
		b.WriteString(tui.StyleSuccess.Render("|"))
	} else {
		b.WriteString(tui.StyleMuted.Render("|"))
	}
	b.WriteString("\n")

	// Row 3: Main flow
	b.WriteString("  ")
	b.WriteString(m.renderNode("SQLite", storeActive))
	edge2 := m.edgeActive("store-daemon") || m.edgeActive("daemon-store")
	if edge2 {
		b.WriteString(tui.StyleSuccess.Render(" <---> "))
	} else {
		b.WriteString(tui.StyleMuted.Render(" ----- "))
	}
	b.WriteString(m.renderNode("athenad", daemonActive))
	edge3 := m.edgeActive("daemon-agent") || m.edgeActive("agent-daemon")
	if edge3 {
		b.WriteString(tui.StyleSuccess.Render(" <---> "))
	} else {
		b.WriteString(tui.StyleMuted.Render(" ----- "))
	}
	b.WriteString(m.renderNode("Claude", agentActive))
	b.WriteString("\n\n")

	// Legend
	b.WriteString(tui.StyleMuted.Render("  Legend: "))
	b.WriteString(tui.StyleSuccess.Render("[active] "))
	b.WriteString(tui.StyleMuted.Render("[idle] "))
	b.WriteString(tui.StyleSuccess.Render("<---> "))
	b.WriteString(tui.StyleMuted.Render("data flow"))
	b.WriteString("\n\n")

	// Event type breakdown
	b.WriteString(tui.StyleTitle.Render("  Event Counts"))
	b.WriteString("\n")
	b.WriteString(tui.Divider(m.width/2))
	b.WriteString("\n")

	// Sort and display event counts
	for eventType, count := range m.eventCounts {
		icon := eventIcon(eventType)
		style := eventStyle(eventType)
		b.WriteString(fmt.Sprintf("  %s %-25s %s\n",
			style.Render(icon),
			style.Render(string(eventType)),
			tui.StyleMuted.Render(fmt.Sprintf("%d", count)),
		))
	}

	return b.String()
}

// renderNode renders a flow diagram node.
func (m *Model) renderNode(label string, active bool) string {
	if active {
		return tui.StyleSuccess.Render(fmt.Sprintf("[%s]", label))
	}
	return tui.StyleMuted.Render(fmt.Sprintf("[%s]", label))
}

// isNodeActive checks if a node has recent activity.
func (m *Model) isNodeActive(nodeID string) bool {
	now := time.Now()
	for id, t := range m.activeEdges {
		if now.Sub(t) < 2*time.Second {
			if strings.Contains(id, nodeID) {
				return true
			}
		}
	}
	return false
}

// edgeActive checks if an edge is currently active.
func (m *Model) edgeActive(edgeID string) bool {
	if t, ok := m.activeEdges[edgeID]; ok {
		return time.Since(t) < 2*time.Second
	}
	return false
}

// renderDetail renders the event detail view.
func (m *Model) renderDetail() string {
	if m.detailEvent == nil {
		return tui.StyleEmptyState.Render("\n  No event selected.\n")
	}

	var b strings.Builder
	e := m.detailEvent

	b.WriteString("\n")
	b.WriteString(tui.StyleTitle.Render("  Event Details"))
	b.WriteString("\n")
	b.WriteString(tui.Divider(m.width))
	b.WriteString("\n\n")

	// Basic fields
	b.WriteString(m.detailField("ID", e.ID))
	b.WriteString(m.detailField("Timestamp", e.Timestamp.Format(time.RFC3339Nano)))
	b.WriteString(m.detailField("Type", string(e.Type)))
	b.WriteString(m.detailField("Source", string(e.Source)))

	if e.AgentID != "" {
		b.WriteString(m.detailField("Agent ID", e.AgentID))
	}
	if e.WorktreePath != "" {
		b.WriteString(m.detailField("Worktree", e.WorktreePath))
	}

	// Payload
	if len(e.Payload) > 0 {
		b.WriteString("\n")
		b.WriteString(tui.StyleTitle.Render("  Payload"))
		b.WriteString("\n")
		b.WriteString(tui.Divider(m.width/2))
		b.WriteString("\n\n")

		// Pretty print JSON
		var pretty map[string]any
		if json.Unmarshal(e.Payload, &pretty) == nil {
			formatted, _ := json.MarshalIndent(pretty, "  ", "  ")
			// Truncate if too long
			lines := strings.Split(string(formatted), "\n")
			maxLines := m.height - 15
			if maxLines < 5 {
				maxLines = 5
			}
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				lines = append(lines, "  ... (truncated)")
			}
			b.WriteString(tui.StyleSecondary.Render(strings.Join(lines, "\n")))
		} else {
			// Raw payload
			payload := string(e.Payload)
			if len(payload) > 500 {
				payload = payload[:500] + "..."
			}
			b.WriteString(tui.StyleSecondary.Render("  " + payload))
		}
	}

	return b.String()
}

// detailField renders a labeled field.
func (m *Model) detailField(label, value string) string {
	return fmt.Sprintf("  %s %s\n",
		tui.StyleMuted.Render(fmt.Sprintf("%-12s:", label)),
		tui.StyleNormal.Render(value),
	)
}

// --- Style helpers ---

// sourceIcon returns an icon for a stream source.
func sourceIcon(source control.StreamSource) string {
	switch source {
	case control.StreamSourceDaemon:
		return "D "
	case control.StreamSourceAgent:
		return "A "
	case control.StreamSourceStore:
		return "S "
	case control.StreamSourceAPI:
		return "@ "
	case control.StreamSourceClient:
		return "C "
	default:
		return "? "
	}
}

// sourceStyle returns a style for a stream source.
func sourceStyle(source control.StreamSource) lipgloss.Style {
	switch source {
	case control.StreamSourceDaemon:
		return tui.StyleInfo
	case control.StreamSourceAgent:
		return tui.StyleSuccess
	case control.StreamSourceStore:
		return tui.StyleWarning
	case control.StreamSourceAPI:
		return tui.StyleAccent
	default:
		return tui.StyleMuted
	}
}

// eventIcon returns an icon for an event type.
func eventIcon(t control.StreamEventType) string {
	switch t {
	case control.StreamEventAgentCreated, control.StreamEventAgentStarted:
		return "+ "
	case control.StreamEventAgentTerminated:
		return "- "
	case control.StreamEventAgentCrashed:
		return "X "
	case control.StreamEventAgentAwaiting:
		return "? "
	case control.StreamEventThinking:
		return ". "
	case control.StreamEventToolCall:
		return "> "
	case control.StreamEventToolResult:
		return "< "
	case control.StreamEventMessage:
		return "# "
	case control.StreamEventHeartbeat:
		return "~ "
	case control.StreamEventJobCreated:
		return "+ "
	case control.StreamEventJobCompleted:
		return "v "
	case control.StreamEventJobFailed:
		return "X "
	case control.StreamEventAPICall:
		return "> "
	case control.StreamEventAPIResponse:
		return "< "
	default:
		return "  "
	}
}

// eventStyle returns a style for an event type.
func eventStyle(t control.StreamEventType) lipgloss.Style {
	switch t {
	case control.StreamEventAgentCreated, control.StreamEventAgentStarted,
		control.StreamEventJobCompleted, control.StreamEventToolResult:
		return tui.StyleSuccess
	case control.StreamEventAgentCrashed, control.StreamEventJobFailed:
		return tui.StyleDanger
	case control.StreamEventAgentAwaiting, control.StreamEventThinking:
		return tui.StyleWarning
	case control.StreamEventToolCall, control.StreamEventAPICall:
		return tui.StyleInfo
	case control.StreamEventHeartbeat:
		return tui.StyleMuted
	default:
		return tui.StyleNormal
	}
}

// truncate shortens a string to max length.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
