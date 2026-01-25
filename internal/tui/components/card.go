// Package components provides reusable TUI building blocks.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/tui"
)

// Card is a bordered container with optional title.
type Card struct {
	Title       string
	Content     []string
	Width       int
	BorderColor lipgloss.Color
	Selected    bool
	Padding     int // Internal padding (default 1)
}

// NewCard creates a card with default styling.
func NewCard(title string, content ...string) *Card {
	return &Card{
		Title:       title,
		Content:     content,
		Width:       0, // Auto-size
		BorderColor: tui.ColorFgMuted,
		Selected:    false,
		Padding:     1,
	}
}

// WithWidth sets the card width.
func (c *Card) WithWidth(w int) *Card {
	c.Width = w
	return c
}

// WithBorderColor sets the border color.
func (c *Card) WithBorderColor(color lipgloss.Color) *Card {
	c.BorderColor = color
	return c
}

// WithSelected marks the card as selected.
func (c *Card) WithSelected(selected bool) *Card {
	c.Selected = selected
	return c
}

// WithPadding sets the internal padding.
func (c *Card) WithPadding(p int) *Card {
	c.Padding = p
	return c
}

// Render returns the styled card string.
func (c *Card) Render() string {
	borderColor := c.BorderColor
	if c.Selected {
		borderColor = tui.ColorAccent
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, c.Padding)

	if c.Width > 0 {
		style = style.Width(c.Width)
	}

	// Build content
	var sb strings.Builder

	// Title line
	if c.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(tui.ColorFg).
			Bold(true)
		sb.WriteString(titleStyle.Render(c.Title))
		if len(c.Content) > 0 {
			sb.WriteString("\n")
		}
	}

	// Content lines
	for i, line := range c.Content {
		sb.WriteString(line)
		if i < len(c.Content)-1 {
			sb.WriteString("\n")
		}
	}

	return style.Render(sb.String())
}

// ProjectCard renders a project overview card with stats.
type ProjectCard struct {
	Name       string
	Worktrees  int
	Agents     int
	Running    int
	Awaiting   int
	CacheRate  float64 // 0-1
	LastActive string  // Human-readable time ago
	Status     string  // "healthy", "idle", "attention"
	Selected   bool
	Width      int
}

// Render returns the styled project card.
func (p *ProjectCard) Render() string {
	borderColor := tui.ColorFgMuted
	if p.Selected {
		borderColor = tui.ColorAccent
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	if p.Width > 0 {
		style = style.Width(p.Width)
	}

	// Calculate inner width (card width - borders - padding)
	innerWidth := p.Width - 4 // 2 for borders, 2 for padding
	if innerWidth < 20 {
		innerWidth = 20
	}

	var sb strings.Builder

	// Title row: name on left, status on right
	nameStyle := lipgloss.NewStyle().Foreground(tui.ColorFg).Bold(true)
	statusIcon, statusColor := p.statusIndicator()
	statusStyle := lipgloss.NewStyle().Foreground(statusColor)
	statusText := statusStyle.Render(statusIcon + " " + p.Status)

	// Calculate padding between name and status
	nameLen := lipgloss.Width(p.Name)
	statusLen := lipgloss.Width(statusText)
	padding := innerWidth - nameLen - statusLen
	if padding < 2 {
		padding = 2
	}

	sb.WriteString(nameStyle.Render(p.Name))
	sb.WriteString(strings.Repeat(" ", padding))
	sb.WriteString(statusText)
	sb.WriteString("\n")

	// Stats row: worktrees │ agents │ last active
	statsStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
	worktreeText := "worktree"
	if p.Worktrees != 1 {
		worktreeText = "worktrees"
	}
	agentText := "agent"
	if p.Agents != 1 {
		agentText = "agents"
	}

	stats := []string{}
	stats = append(stats, statsStyle.Render(formatInt(p.Worktrees)+" "+worktreeText))

	// Agent count with running info
	agentInfo := formatInt(p.Agents) + " " + agentText
	if p.Running > 0 {
		agentInfo += " (" + formatInt(p.Running) + " active)"
	}
	stats = append(stats, statsStyle.Render(agentInfo))

	// Last active
	if p.LastActive != "" {
		mutedStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
		stats = append(stats, mutedStyle.Render(p.LastActive))
	}

	sb.WriteString(strings.Join(stats, "  │  "))

	return style.Render(sb.String())
}

func (p *ProjectCard) statusIndicator() (string, lipgloss.Color) {
	switch p.Status {
	case "healthy":
		return "*", tui.ColorSuccess
	case "attention":
		return "!", tui.ColorWarning
	case "idle":
		return "-", tui.ColorFgMuted
	default:
		return "-", tui.ColorFgMuted
	}
}

func formatInt(n int) string {
	return strings.TrimSpace(lipgloss.NewStyle().Render(itoa(n)))
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
