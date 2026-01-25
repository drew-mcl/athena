package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/tui"
)

// ProgressBar renders a horizontal progress indicator.
type ProgressBar struct {
	Current   int
	Max       int
	Width     int
	Color     lipgloss.Color
	ShowLabel bool // Show percentage
	ShowValue bool // Show "45K / 100K"
	Label     string
}

// NewProgressBar creates a progress bar with defaults.
func NewProgressBar(current, max int) *ProgressBar {
	return &ProgressBar{
		Current:   current,
		Max:       max,
		Width:     20,
		Color:     tui.ColorSuccess,
		ShowLabel: false,
		ShowValue: false,
	}
}

// WithWidth sets the bar width.
func (p *ProgressBar) WithWidth(w int) *ProgressBar {
	p.Width = w
	return p
}

// WithColor sets the filled portion color.
func (p *ProgressBar) WithColor(c lipgloss.Color) *ProgressBar {
	p.Color = c
	return p
}

// WithShowLabel enables percentage display.
func (p *ProgressBar) WithShowLabel(show bool) *ProgressBar {
	p.ShowLabel = show
	return p
}

// WithShowValue enables value display (e.g., "45K / 100K").
func (p *ProgressBar) WithShowValue(show bool) *ProgressBar {
	p.ShowValue = show
	return p
}

// WithLabel sets a custom label prefix.
func (p *ProgressBar) WithLabel(label string) *ProgressBar {
	p.Label = label
	return p
}

// Render returns the styled progress bar string.
func (p *ProgressBar) Render() string {
	if p.Max <= 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
		return emptyStyle.Render(strings.Repeat("░", p.Width))
	}

	pct := float64(p.Current) / float64(p.Max)
	if pct > 1 {
		pct = 1
	}
	if pct < 0 {
		pct = 0
	}

	filled := int(pct * float64(p.Width))
	empty := p.Width - filled

	filledStyle := lipgloss.NewStyle().Foreground(p.Color)
	emptyStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)

	var sb strings.Builder

	if p.Label != "" {
		labelStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
		sb.WriteString(labelStyle.Render(p.Label))
		sb.WriteString(" ")
	}

	sb.WriteString(filledStyle.Render(strings.Repeat("█", filled)))
	sb.WriteString(emptyStyle.Render(strings.Repeat("░", empty)))

	if p.ShowLabel {
		pctStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
		sb.WriteString(pctStyle.Render(fmt.Sprintf(" %d%%", int(pct*100))))
	}

	if p.ShowValue {
		valStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
		sb.WriteString(valStyle.Render(fmt.Sprintf(" %s / %s", formatCount(p.Current), formatCount(p.Max))))
	}

	return sb.String()
}

// formatCount formats large numbers with K suffix.
func formatCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// TokenProgress renders a token usage progress bar with specific styling.
type TokenProgress struct {
	Used      int
	Max       int
	CacheRate float64 // 0-1
	Width     int
}

// NewTokenProgress creates a token progress indicator.
func NewTokenProgress(used, max int, cacheRate float64) *TokenProgress {
	return &TokenProgress{
		Used:      used,
		Max:       max,
		CacheRate: cacheRate,
		Width:     20,
	}
}

// WithWidth sets the bar width.
func (t *TokenProgress) WithWidth(w int) *TokenProgress {
	t.Width = w
	return t
}

// Render returns the styled token progress string.
func (t *TokenProgress) Render() string {
	bar := NewProgressBar(t.Used, t.Max).
		WithWidth(t.Width).
		WithColor(tui.ColorInfo).
		WithShowValue(true)

	result := bar.Render()

	if t.CacheRate > 0 {
		cacheStyle := lipgloss.NewStyle().Foreground(tui.ColorSuccess)
		result += cacheStyle.Render(fmt.Sprintf("  (%d%% cached)", int(t.CacheRate*100)))
	}

	return result
}

// CostProgress renders a cost/budget progress bar.
type CostProgress struct {
	Current float64
	Budget  float64
	Width   int
}

// NewCostProgress creates a cost progress indicator.
func NewCostProgress(current, budget float64) *CostProgress {
	return &CostProgress{
		Current: current,
		Budget:  budget,
		Width:   20,
	}
}

// WithWidth sets the bar width.
func (c *CostProgress) WithWidth(w int) *CostProgress {
	c.Width = w
	return c
}

// Render returns the styled cost progress string.
func (c *CostProgress) Render() string {
	pct := c.Current / c.Budget
	color := tui.ColorSuccess
	if pct > 0.8 {
		color = tui.ColorWarning
	}
	if pct > 1.0 {
		color = tui.ColorDanger
	}

	bar := NewProgressBar(int(c.Current*100), int(c.Budget*100)).
		WithWidth(c.Width).
		WithColor(color)

	valStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
	return bar.Render() + valStyle.Render(fmt.Sprintf("  $%.2f / $%.2f", c.Current, c.Budget))
}
