// Package layout provides flexible table layout utilities for the TUI.
package layout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/tui"
)

// Spacing constants for visual rhythm
const (
	PaddingLeft  = 3 // Left margin for content
	PaddingRight = 2 // Right margin
	ColumnGap    = 2 // Space between columns
)

// Column defines a flexible table column
type Column struct {
	Header   string
	MinWidth int
	MaxWidth int // 0 = unlimited
	Flex     int // Weight for extra space distribution (0 = fixed)
}

// Table manages responsive column layouts
type Table struct {
	Columns   []Column
	Width     int
	columnW   []int // Calculated widths
	headerRow string
}

// NewTable creates a table with the given columns
func NewTable(columns []Column) *Table {
	return &Table{
		Columns: columns,
	}
}

// SetWidth updates the table width and recalculates column widths
func (t *Table) SetWidth(width int) {
	t.Width = width
	t.calculateWidths()
}

// calculateWidths distributes available width among columns
func (t *Table) calculateWidths() {
	if len(t.Columns) == 0 || t.Width <= 0 {
		return
	}

	// Calculate available width (minus padding and gaps)
	available := t.Width - PaddingLeft - PaddingRight
	gapSpace := ColumnGap * (len(t.Columns) - 1)
	available -= gapSpace

	if available < 0 {
		available = 0
	}

	// First pass: assign minimum widths
	t.columnW = make([]int, len(t.Columns))
	for i, col := range t.Columns {
		t.columnW[i] = col.MinWidth
	}

	// Multiple passes to distribute space (handles max-width capping)
	for passes := 0; passes < 3; passes++ {
		// Calculate remaining flex and used space
		usedSpace := 0
		totalFlex := 0
		for i, col := range t.Columns {
			usedSpace += t.columnW[i]
			// Only count flex for columns not at max
			if col.Flex > 0 && (col.MaxWidth == 0 || t.columnW[i] < col.MaxWidth) {
				totalFlex += col.Flex
			}
		}

		extra := available - usedSpace
		if extra <= 0 || totalFlex == 0 {
			break
		}

		// Distribute extra space
		for i, col := range t.Columns {
			if col.Flex > 0 && (col.MaxWidth == 0 || t.columnW[i] < col.MaxWidth) {
				share := (extra * col.Flex) / totalFlex
				newWidth := t.columnW[i] + share

				// Respect max width if set
				if col.MaxWidth > 0 && newWidth > col.MaxWidth {
					newWidth = col.MaxWidth
				}
				t.columnW[i] = newWidth
			}
		}
	}

	// Build header row
	t.headerRow = t.buildHeaderRow()
}

// buildHeaderRow creates the formatted header
func (t *Table) buildHeaderRow() string {
	var parts []string
	for i, col := range t.Columns {
		w := t.columnW[i]
		header := col.Header
		if len(header) > w {
			header = header[:w]
		}
		parts = append(parts, padRight(header, w))
	}

	row := strings.Repeat(" ", PaddingLeft) + strings.Join(parts, strings.Repeat(" ", ColumnGap))
	return tui.StyleColumnHeader.Render(row)
}

// RenderHeader returns the formatted header row
func (t *Table) RenderHeader() string {
	if t.headerRow == "" {
		t.calculateWidths()
	}
	return t.headerRow
}

// RenderRow formats a data row with the calculated column widths
func (t *Table) RenderRow(values []string, selected bool) string {
	var parts []string
	for i, w := range t.columnW {
		val := ""
		if i < len(values) {
			val = values[i]
		}

		// Get visible width (ignores ANSI codes)
		visibleW := lipgloss.Width(val)

		// Truncate if needed (only for unstyled text)
		if visibleW > w {
			// For styled text, truncation is complex - just use as-is
			// For plain text, truncate
			if visibleW == len(val) { // No ANSI codes
				if w > 1 {
					val = val[:w-1] + "…"
				} else {
					val = val[:w]
				}
			}
		}

		// Pad to width using visible width
		if visibleW < w {
			val = val + strings.Repeat(" ", w-visibleW)
		}
		parts = append(parts, val)
	}

	// Build row with selection indicator
	indicator := "   " // 3 spaces (matches PaddingLeft)
	if selected {
		indicator = tui.StyleSelectedIndicator.Render(" ▌ ")
	}

	content := strings.Join(parts, strings.Repeat(" ", ColumnGap))

	if selected {
		return indicator + tui.StyleSelected.Render(content)
	}
	return indicator + content
}

// RenderRowStyled formats a data row with pre-styled values
func (t *Table) RenderRowStyled(values []string, selected bool) string {
	// For styled content, we need to handle width differently
	// since lipgloss.Width accounts for ANSI codes
	var parts []string
	for i, w := range t.columnW {
		val := ""
		if i < len(values) {
			val = values[i]
		}

		// Get visible width
		visibleW := lipgloss.Width(val)
		if visibleW > w {
			// Truncate - this is tricky with ANSI codes
			// For now, just use the raw truncation
			if w > 1 {
				val = truncateStyled(val, w-1) + "…"
			}
		} else {
			// Pad to width
			val = val + strings.Repeat(" ", w-visibleW)
		}
		parts = append(parts, val)
	}

	indicator := "   "
	if selected {
		indicator = tui.StyleSelectedIndicator.Render(" ▌ ")
	}

	content := strings.Join(parts, strings.Repeat(" ", ColumnGap))

	if selected {
		// Apply selection background to the whole row
		return indicator + tui.StyleSelected.Render(stripAnsi(content))
	}
	return indicator + content
}

// ColumnWidths returns the calculated column widths
func (t *Table) ColumnWidths() []int {
	return t.columnW
}

// Divider returns a full-width divider line
func Divider(width int) string {
	if width <= 0 {
		width = 80
	}
	return tui.StyleDivider.Render(strings.Repeat("─", width))
}

// padRight pads a string to the specified width
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// truncateStyled attempts to truncate styled text
// Note: This is a simplified version that may not handle all ANSI sequences
func truncateStyled(s string, maxLen int) string {
	// For simplicity, strip ANSI and truncate
	plain := stripAnsi(s)
	if len(plain) <= maxLen {
		return s
	}
	return plain[:maxLen]
}

// stripAnsi removes ANSI escape codes from a string
func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false

	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// ContentHeight calculates available content height given terminal dimensions
// Accounts for header, divider, column headers, and footer
func ContentHeight(termHeight int) int {
	// Layout:
	// - Header line (1)
	// - Divider (1)
	// - Breathing room (1)
	// - Column headers (1)
	// - [Content area]
	// - Footer line (1)
	// Total overhead: 5 lines
	const overhead = 5

	return max(1, termHeight-overhead)
}

// PinFooterToBottom ensures footer is pinned to the bottom of the terminal.
// It calculates the padding needed between content and footer to fill the
// terminal height. If content is too tall, it truncates from the bottom
// (preserving header).
func PinFooterToBottom(content, footer string, termHeight int) string {
	contentLines := strings.Split(content, "\n")
	footerLines := strings.Split(footer, "\n")

	contentHeight := len(contentLines)
	footerHeight := len(footerLines)

	paddingNeeded := termHeight - contentHeight - footerHeight

	// If content too tall, truncate from end (keep header)
	if paddingNeeded < 0 {
		keepLines := max(1, termHeight-footerHeight)
		if keepLines < len(contentLines) {
			contentLines = contentLines[:keepLines]
		}
		paddingNeeded = 0
	}

	var b strings.Builder
	b.WriteString(strings.Join(contentLines, "\n"))
	for i := 0; i < paddingNeeded; i++ {
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(footerLines, "\n"))
	return b.String()
}

// ScrollWindow calculates visible range for a scrollable list
type ScrollWindow struct {
	Offset      int
	VisibleRows int
	TotalItems  int
	HasMore     bool
	HasLess     bool
}

// CalculateScrollWindow determines which items are visible given scroll state
func CalculateScrollWindow(totalItems, selected, visibleRows int) ScrollWindow {
	if totalItems == 0 || visibleRows <= 0 {
		return ScrollWindow{}
	}

	offset := 0

	// Keep selected item in view with some context
	if selected >= visibleRows {
		offset = selected - visibleRows + 1
	}

	// Ensure we don't scroll past the end
	maxOffset := max(0, totalItems-visibleRows)
	offset = min(offset, maxOffset)

	return ScrollWindow{
		Offset:      offset,
		VisibleRows: visibleRows,
		TotalItems:  totalItems,
		HasMore:     offset+visibleRows < totalItems,
		HasLess:     offset > 0,
	}
}
