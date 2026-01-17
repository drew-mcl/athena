// Package tui provides the terminal user interface for Athena.
package tui

import "github.com/charmbracelet/lipgloss"

// Tokyo Night inspired color palette - refined for clarity
var (
	// Base tones
	ColorBg        = lipgloss.Color("#1a1b26") // Deep background
	ColorBgSurface = lipgloss.Color("#24283b") // Elevated surfaces (cards, selections)
	ColorBgHover   = lipgloss.Color("#292e42") // Interactive hover states

	// Text hierarchy
	ColorFg          = lipgloss.Color("#c0caf5") // Primary text
	ColorFgSecondary = lipgloss.Color("#a9b1d6") // Secondary info
	ColorFgMuted     = lipgloss.Color("#565f89") // Disabled/tertiary

	// Status colors - Tokyo Night palette
	ColorSuccess = lipgloss.Color("#9ece6a") // Running/active - green
	ColorInfo    = lipgloss.Color("#7aa2f7") // Planning/info - blue
	ColorWarning = lipgloss.Color("#e0af68") // Pending/awaiting - amber
	ColorDanger  = lipgloss.Color("#f7768e") // Crashed/error - red/pink
	ColorNeutral = lipgloss.Color("#565f89") // Completed/idle - muted

	// Accent - warm copper for brand identity
	ColorAccent    = lipgloss.Color("#d4a373") // Logo, active tab, key highlights
	ColorAccentDim = lipgloss.Color("#a67744") // Softer accent for borders
)

// StatusIcons - Starship-compatible ASCII symbols (work in all terminals)
var StatusIcons = map[string]string{
	"running":    "*", // Active
	"planning":   "~", // Thinking
	"executing":  ">", // In progress
	"awaiting":   ".", // Waiting
	"crashed":    "x", // Failed
	"pending":    "-", // Queued
	"completed":  "✓", // Done (widely supported)
	"terminated": "#", // Stopped
	"idle":       "-", // Inactive
}

// AgentIcons provides backwards-compatible agent icons (maps to StatusIcons)
var AgentIcons = StatusIcons

// StatusColor returns the color for a given status
func StatusColor(status string) lipgloss.Color {
	switch status {
	case "running", "executing":
		return ColorSuccess
	case "planning":
		return ColorInfo
	case "crashed":
		return ColorDanger
	case "pending", "awaiting":
		return ColorWarning
	case "completed", "terminated":
		return ColorNeutral
	default:
		return ColorFgMuted
	}
}

// StatusStyle returns styled text for a status
func StatusStyle(status string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(StatusColor(status))
}

// Core styles with proper hierarchy
var (
	// Logo and branding
	StyleLogo = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	// Tab navigation
	StyleTabActive = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColorFgMuted)

	// Column headers
	StyleColumnHeader = lipgloss.NewStyle().
				Foreground(ColorFgMuted).
				Bold(true)

	// Selection highlighting
	StyleSelected = lipgloss.NewStyle().
			Background(ColorBgSurface).
			Foreground(ColorFg).
			Bold(true)

	StyleSelectedIndicator = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	// Dividers and borders
	StyleDivider = lipgloss.NewStyle().
			Foreground(ColorFgMuted)

	StyleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorFgMuted).
			Padding(1, 2)

	// Text styles
	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorFg).
			Bold(true)

	StyleHeader = lipgloss.NewStyle().
			Foreground(ColorFgMuted).
			Bold(true)

	StyleNormal = lipgloss.NewStyle().
			Foreground(ColorFg)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorFgMuted)

	StyleSecondary = lipgloss.NewStyle().
			Foreground(ColorFgSecondary)

	StyleAccent = lipgloss.NewStyle().
			Foreground(ColorAccent)

	// Help and footer
	StyleHelp = lipgloss.NewStyle().
			Foreground(ColorFgMuted)

	StyleHelpKey = lipgloss.NewStyle().
			Foreground(ColorFgSecondary)

	StyleStatus = lipgloss.NewStyle().
			Foreground(ColorFgSecondary)

	// Empty states
	StyleEmptyState = lipgloss.NewStyle().
			Foreground(ColorFgMuted).
			Italic(true)

	// Status message feedback
	StyleStatusMsg = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StyleErrorMsg = lipgloss.NewStyle().
			Foreground(ColorDanger)

	StyleSuccessMsg = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StyleInfo = lipgloss.NewStyle().
		Foreground(ColorInfo)

	StyleNeutral = lipgloss.NewStyle().
		Foreground(ColorNeutral)

	StyleSuccess = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	StyleDanger = lipgloss.NewStyle().
		Foreground(ColorDanger)
)

// Logo returns the ASCII art logo
func Logo() string {
	return StyleLogo.Render("▐▛▜▌")
}

// Divider returns a full-width horizontal divider
func Divider(width int) string {
	if width <= 0 {
		width = 80
	}
	line := ""
	for i := 0; i < width; i++ {
		line += "─"
	}
	return StyleDivider.Render(line)
}
