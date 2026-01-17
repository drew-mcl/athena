// Package tui provides the terminal user interface for Athena.
package tui

import "github.com/charmbracelet/lipgloss"

// Tokyo Night inspired color palette
var (
	ColorBg       = lipgloss.Color("#1a1b26")
	ColorBgAlt    = lipgloss.Color("#24283b")
	ColorFg       = lipgloss.Color("#c0caf5")
	ColorFgMuted  = lipgloss.Color("#565f89")
	ColorRunning  = lipgloss.Color("#9ece6a")
	ColorPlanning = lipgloss.Color("#7aa2f7")
	ColorCrashed  = lipgloss.Color("#f7768e")
	ColorPending  = lipgloss.Color("#e0af68")
	ColorCompleted = lipgloss.Color("#565f89")
	ColorAccent   = lipgloss.Color("#d4a373")
)

// Agent status icons
var AgentIcons = map[string]string{
	"running":    "▐▛▜▌",
	"planning":   "▐▛▜▌",
	"executing":  "▐▛▜▌",
	"awaiting":   "▐▛▜▌",
	"crashed":    "▐▄▄▌",
	"pending":    "▐░░▌",
	"completed":  "▐▀▀▌",
	"terminated": "▐▀▀▌",
	"idle":       "─",
}

// StatusColor returns the color for a given status
func StatusColor(status string) lipgloss.Color {
	switch status {
	case "running", "executing":
		return ColorRunning
	case "planning":
		return ColorPlanning
	case "crashed":
		return ColorCrashed
	case "pending", "awaiting":
		return ColorPending
	case "completed", "terminated":
		return ColorCompleted
	default:
		return ColorFgMuted
	}
}

// Common styles
var (
	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorFg).
			Bold(true).
			MarginBottom(1)

	StyleHeader = lipgloss.NewStyle().
			Foreground(ColorFgMuted).
			Bold(true)

	StyleSelected = lipgloss.NewStyle().
			Background(ColorBgAlt).
			Foreground(ColorFg)

	StyleNormal = lipgloss.NewStyle().
			Foreground(ColorFg)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorFgMuted)

	StyleAccent = lipgloss.NewStyle().
			Foreground(ColorAccent)

	StyleHelp = lipgloss.NewStyle().
			Foreground(ColorFgMuted).
			MarginTop(1)

	StyleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorFgMuted).
			Padding(1, 2)
)

// StatusStyle returns styled text for a status
func StatusStyle(status string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(StatusColor(status))
}

// Logo returns the ASCII art logo
func Logo() string {
	return StyleAccent.Render("▐▛███▜▌") + "\n" + StyleAccent.Render("▝▜███▛▘")
}
