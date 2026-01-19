// Package layout provides flexible table layout utilities for the TUI.
package layout

import (
	"fmt"
	"strings"

	"github.com/drewfead/athena/internal/tui"
)

// TableListOptions configures rendering a table-backed list.
type TableListOptions struct {
	Table              *Table
	HeaderRenderer     func() string
	TotalItems         int
	Selected           int
	ContentHeight      int
	EmptyMessage       string
	RowRenderer        func(index int, selected bool) string
	ScrollUpRenderer   func(offset int) string
	ScrollDownRenderer func(remaining int) string
	// RowHeight is the number of lines each row occupies. Defaults to 1.
	// Set to 2 for rows with subtitles (e.g., worktrees with plan summaries).
	RowHeight int
}

// RenderTableList renders a table header plus scrollable list content.
func RenderTableList(opts TableListOptions) string {
	if opts.Table == nil {
		return ""
	}

	var b strings.Builder
	header := opts.Table.RenderHeader()
	if opts.HeaderRenderer != nil {
		header = opts.HeaderRenderer()
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	if opts.TotalItems == 0 || opts.RowRenderer == nil {
		if opts.EmptyMessage != "" {
			b.WriteString(tui.StyleEmptyState.Render(opts.EmptyMessage))
			b.WriteString("\n")
		}
		emptyPad := opts.ContentHeight - 2
		if emptyPad < 0 {
			emptyPad = 0
		}
		for i := 0; i < emptyPad; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Default row height to 1 if not specified
	rowHeight := opts.RowHeight
	if rowHeight < 1 {
		rowHeight = 1
	}

	// Calculate visible items accounting for row height
	visibleLines := opts.ContentHeight - 1
	if visibleLines < 1 {
		visibleLines = 1
	}
	visibleItems := visibleLines / rowHeight
	if visibleItems < 1 {
		visibleItems = 1
	}
	scroll := CalculateScrollWindow(opts.TotalItems, opts.Selected, visibleItems)

	if scroll.HasLess {
		indicator := fmt.Sprintf("   ^ %d more", scroll.Offset)
		if opts.ScrollUpRenderer != nil {
			indicator = opts.ScrollUpRenderer(scroll.Offset)
		}
		b.WriteString(tui.StyleMuted.Render(indicator))
		b.WriteString("\n")
	}

	end := scroll.Offset + scroll.VisibleRows
	if end > opts.TotalItems {
		end = opts.TotalItems
	}

	for i := scroll.Offset; i < end; i++ {
		b.WriteString(opts.RowRenderer(i, i == opts.Selected))
		b.WriteString("\n")
	}

	if scroll.HasMore {
		remaining := opts.TotalItems - end
		indicator := fmt.Sprintf("   v %d more", remaining)
		if opts.ScrollDownRenderer != nil {
			indicator = opts.ScrollDownRenderer(remaining)
		}
		b.WriteString(tui.StyleMuted.Render(indicator))
		b.WriteString("\n")
	}

	return b.String()
}
