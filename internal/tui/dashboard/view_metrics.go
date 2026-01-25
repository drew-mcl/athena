package dashboard

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui"
)

// renderMetricsPanel renders a visual metrics panel for an agent.
// Format inspired by Gemini CLI metrics display.
func (m Model) renderMetricsPanel(metrics *control.AgentMetrics) string {
	if metrics == nil {
		return tui.StyleMuted.Render("  No metrics available yet")
	}

	var b strings.Builder

	b.WriteString(tui.StyleAccent.Render("  Usage Metrics"))
	b.WriteString("\n\n")

	// Token usage section with visual bar
	b.WriteString(renderTokenSection(metrics, m.width-6))
	b.WriteString("\n")

	// Duration section
	b.WriteString(renderDurationSection(metrics))
	b.WriteString("\n")

	// Tool calls section
	b.WriteString(renderToolSection(metrics))
	b.WriteString("\n")

	// Cost section (if available)
	if metrics.CostCents > 0 {
		b.WriteString(renderCostSection(metrics))
		b.WriteString("\n")
	}

	return b.String()
}

// renderTokenSection displays token usage with visual progress bars.
func renderTokenSection(metrics *control.AgentMetrics, maxWidth int) string {
	var b strings.Builder

	b.WriteString(tui.StyleMuted.Render("  Tokens"))
	b.WriteString("\n")

	// Calculate total for percentages
	totalTokens := metrics.InputTokens + metrics.OutputTokens
	if totalTokens == 0 {
		totalTokens = metrics.TotalTokens
	}
	if totalTokens == 0 {
		b.WriteString(tui.StyleMuted.Render("  └── No token data"))
		return b.String()
	}

	// Bar width (leaving room for labels and tree chars)
	barWidth := min(24, maxWidth-40)
	if barWidth < 10 {
		barWidth = 10
	}

	// Input tokens with bar
	inputPct := float64(metrics.InputTokens) / float64(totalTokens)
	inputBar := renderProgressBar(inputPct, barWidth)
	b.WriteString(fmt.Sprintf("  ├── Input:  %8s  %s\n",
		formatTokenCount(metrics.InputTokens),
		inputBar))

	// Cached tokens (if any)
	if metrics.CacheReads > 0 {
		cacheHitRate := metrics.CacheHitRate
		if cacheHitRate == 0 && metrics.InputTokens > 0 {
			// Calculate cache hit rate if not provided
			cacheHitRate = float64(metrics.CacheReads) / float64(metrics.InputTokens) * 100
		}
		cacheStr := fmt.Sprintf("(%d%% hit rate)", int(cacheHitRate))
		b.WriteString(fmt.Sprintf("  ├── Cached: %8s  %s\n",
			formatTokenCount(metrics.CacheReads),
			tui.StyleSuccess.Render(cacheStr)))
	}

	// Cache creation (if any)
	if metrics.CacheCreation > 0 {
		b.WriteString(fmt.Sprintf("  ├── Cache+: %8s  %s\n",
			formatTokenCount(metrics.CacheCreation),
			tui.StyleMuted.Render("(created)")))
	}

	// Output tokens with bar
	outputPct := float64(metrics.OutputTokens) / float64(totalTokens)
	outputBar := renderProgressBar(outputPct, barWidth)
	b.WriteString(fmt.Sprintf("  └── Output: %8s  %s\n",
		formatTokenCount(metrics.OutputTokens),
		outputBar))

	return b.String()
}

// renderProgressBar creates a visual progress bar using Unicode block characters.
func renderProgressBar(pct float64, width int) string {
	if width <= 0 {
		return ""
	}

	filled := int(math.Round(pct * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	empty := width - filled

	// Use block characters for the bar
	filledStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", empty)

	return tui.StyleInfo.Render(filledStr) + tui.StyleMuted.Render(emptyStr)
}

// renderDurationSection displays timing information.
func renderDurationSection(metrics *control.AgentMetrics) string {
	var b strings.Builder

	wallTime := time.Duration(metrics.DurationMs) * time.Millisecond
	apiTime := time.Duration(metrics.APITimeMs) * time.Millisecond

	// Format wall time
	wallStr := formatDurationHuman(wallTime)

	b.WriteString("  ")
	b.WriteString(tui.StyleMuted.Render("Duration: "))
	b.WriteString(tui.StyleNormal.Render(wallStr))

	// Show API time if available and different
	if metrics.APITimeMs > 0 && metrics.APITimeMs != metrics.DurationMs {
		apiStr := formatDurationHuman(apiTime)
		b.WriteString(tui.StyleMuted.Render(" (API: "))
		b.WriteString(tui.StyleNormal.Render(apiStr))
		b.WriteString(tui.StyleMuted.Render(")"))
	}

	// Show turn count if available
	if metrics.NumTurns > 0 {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf(" - %d turns", metrics.NumTurns)))
	}

	b.WriteString("\n")
	return b.String()
}

// renderToolSection displays tool usage statistics.
func renderToolSection(metrics *control.AgentMetrics) string {
	var b strings.Builder

	if metrics.ToolUseCount == 0 {
		return ""
	}

	b.WriteString("  ")
	b.WriteString(tui.StyleMuted.Render("Tools: "))
	b.WriteString(fmt.Sprintf("%d calls", metrics.ToolUseCount))

	// Calculate success/failure if we have success rate
	if metrics.ToolSuccessRate > 0 {
		successCount := int(float64(metrics.ToolUseCount) * metrics.ToolSuccessRate / 100)
		failCount := metrics.ToolUseCount - successCount

		b.WriteString(" (")
		b.WriteString(tui.StyleSuccess.Render(fmt.Sprintf("✓%d", successCount)))
		if failCount > 0 {
			b.WriteString(" ")
			b.WriteString(tui.StyleDanger.Render(fmt.Sprintf("✗%d", failCount)))
		}
		b.WriteString(")")

		// Show percentage
		pctStr := fmt.Sprintf(" %.0f%% success", metrics.ToolSuccessRate)
		b.WriteString(tui.StyleMuted.Render(pctStr))
	}

	b.WriteString("\n")
	return b.String()
}

// renderCostSection displays estimated cost.
func renderCostSection(metrics *control.AgentMetrics) string {
	var b strings.Builder

	costUSD := float64(metrics.CostCents) / 100.0

	b.WriteString("  ")
	b.WriteString(tui.StyleMuted.Render("Est. Cost: "))

	// Color based on cost
	costStr := fmt.Sprintf("$%.2f", costUSD)
	if costUSD > 1.0 {
		b.WriteString(tui.StyleWarning.Render(costStr))
	} else if costUSD > 0.10 {
		b.WriteString(tui.StyleNormal.Render(costStr))
	} else {
		b.WriteString(tui.StyleSuccess.Render(costStr))
	}

	b.WriteString("\n")
	return b.String()
}

// formatTokenCount formats token numbers with comma separators.
func formatTokenCount(n int) string {
	if n == 0 {
		return "0"
	}

	// Use thousands separators
	str := fmt.Sprintf("%d", n)
	if len(str) <= 3 {
		return str
	}

	// Add commas
	var result []byte
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatDurationHuman formats a duration in human-readable form like "12m 34s".
func formatDurationHuman(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// renderCompactMetrics renders a single-line metrics summary for list views.
func renderCompactMetrics(metrics *control.AgentMetrics) string {
	if metrics == nil {
		return ""
	}

	parts := []string{}

	if metrics.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s tok", formatCompactNumber(metrics.TotalTokens)))
	}

	if metrics.ToolUseCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", metrics.ToolUseCount))
	}

	if metrics.DurationMs > 0 {
		d := time.Duration(metrics.DurationMs) * time.Millisecond
		parts = append(parts, formatDurationHuman(d))
	}

	if len(parts) == 0 {
		return ""
	}

	return tui.StyleMuted.Render(strings.Join(parts, " │ "))
}

// renderFileActivity renders file operation metrics.
func renderFileActivity(metrics *control.AgentMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	if metrics.FilesRead > 0 {
		parts = append(parts, fmt.Sprintf("%dR", metrics.FilesRead))
	}

	if metrics.FilesWritten > 0 {
		parts = append(parts, fmt.Sprintf("%dW", metrics.FilesWritten))
	}

	if metrics.LinesChanged > 0 {
		parts = append(parts, fmt.Sprintf("+%s lines", formatCompactNumber(metrics.LinesChanged)))
	}

	if len(parts) == 0 {
		return ""
	}

	return tui.StyleMuted.Render("Files: ") + strings.Join(parts, " ")
}
