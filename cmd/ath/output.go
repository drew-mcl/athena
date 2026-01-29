package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/drewfead/athena/internal/control"
)

// ═══════════════════════════════════════════════════════════════════════════
// ANSI COLOR CODES - Standard terminal colors
// ═══════════════════════════════════════════════════════════════════════════

const (
	// Styles
	reset     = "\033[0m"
	bold      = "\033[1m"
	dim       = "\033[2m"
	italic    = "\033[3m"
	underline = "\033[4m"

	// Standard colors
	black   = "\033[30m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"

	// Bright colors
	gray         = "\033[90m" // bright black
	brightRed    = "\033[91m"
	brightGreen  = "\033[92m"
	brightYellow = "\033[93m"
	brightBlue   = "\033[94m"
	brightMagenta = "\033[95m"
	brightCyan   = "\033[96m"
	brightWhite  = "\033[97m"
)

// Box drawing characters
const (
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxHorizontal  = "─"
	boxVertical    = "│"
	boxTeeRight    = "├"
	boxTeeLeft     = "┤"
)

// Status indicators
const (
	checkMark = "✓"
	bullet    = "●"
	circle    = "○"
	arrowDown = "↓"
	arrowUp   = "↑"
)

// Work item shapes
const (
	shapeGoal    = "□"
	shapeFeature = "◇"
	shapeTask    = "○"
)

// ═══════════════════════════════════════════════════════════════════════════
// HELPER FUNCTIONS
// ═══════════════════════════════════════════════════════════════════════════

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// ═══════════════════════════════════════════════════════════════════════════
// WORKTREE TABLE - Boxed table layout matching reference design
// ═══════════════════════════════════════════════════════════════════════════

func printWorktreeTable(worktrees []*control.WorktreeInfo) {
	// Filter out main repos
	var filtered []*control.WorktreeInfo
	for _, wt := range worktrees {
		if !wt.IsMain {
			filtered = append(filtered, wt)
		}
	}

	if len(filtered) == 0 {
		fmt.Println(dim + "No worktrees found" + reset)
		return
	}

	// Column widths
	const nameWidth = 20
	const branchWidth = 45
	const statusWidth = 14
	// Inner width: 1 (left pad) + name + 2 (gap) + branch + 2 (gap) + status + 1 (right pad)
	const innerWidth = 1 + nameWidth + 2 + branchWidth + 2 + statusWidth + 1

	// Header: ┌─ Worktrees ────...────┐
	titleText := "Worktrees"
	// Calculate remaining width for right side dashes
	// Total inner = 2 (for "─ " before title) + len(title) + 1 (space after) + dashes
	rightDashes := innerWidth - 2 - len(titleText) - 1
	fmt.Printf("%s%s%s %s%s%s %s%s\n",
		gray, boxTopLeft, boxHorizontal,
		dim+cyan, titleText, reset,
		gray+strings.Repeat(boxHorizontal, rightDashes)+boxTopRight, reset)

	// Column headers
	fmt.Printf("%s%s%s %s%s  %s%s  %s%s %s%s\n",
		gray, boxVertical, reset,
		dim, padRight("NAME", nameWidth),
		padRight("BRANCH", branchWidth),
		padRight("STATUS", statusWidth), reset,
		gray, boxVertical, reset)

	// Separator
	fmt.Printf("%s%s%s%s%s\n",
		gray, boxTeeRight,
		strings.Repeat(boxHorizontal, innerWidth),
		boxTeeLeft, reset)

	// Stats
	cleanCount, changesCount, untrackedCount := 0, 0, 0

	// Rows
	for _, wt := range filtered {
		name := truncate(extractWorktreeName(wt.Path), nameWidth)
		branch := truncate(wt.Branch, branchWidth)

		// Status
		var statusIcon, statusText, statusColor string
		switch {
		case wt.Status == "" || wt.Status == "clean":
			statusIcon, statusText, statusColor = checkMark, "", green
			cleanCount++
		case strings.Contains(wt.Status, "untracked"):
			statusIcon, statusText, statusColor = "?", "untracked", yellow
			untrackedCount++
		default:
			statusIcon, statusText, statusColor = bullet, "changes", yellow
			changesCount++
		}

		plainStatus := statusIcon
		if statusText != "" {
			plainStatus = statusIcon + " " + statusText
		}

		// Print row: NAME dimmed magenta, BRANCH cyan, STATUS colored
		fmt.Printf("%s%s%s %s%s%s  %s%s%s  %s%s%s %s%s%s\n",
			gray, boxVertical, reset,
			dim+magenta, padRight(name, nameWidth), reset,
			cyan, padRight(branch, branchWidth), reset,
			statusColor, padRight(plainStatus, statusWidth), reset,
			gray, boxVertical, reset)
	}

	// Footer
	fmt.Printf("%s%s%s%s%s\n",
		gray, boxBottomLeft,
		strings.Repeat(boxHorizontal, innerWidth),
		boxBottomRight, reset)

	// Summary
	var parts []string
	if cleanCount > 0 {
		parts = append(parts, fmt.Sprintf("%d clean", cleanCount))
	}
	if changesCount > 0 {
		parts = append(parts, fmt.Sprintf("%d with changes", changesCount))
	}
	if untrackedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", untrackedCount))
	}

	fmt.Printf("\n%s%d worktrees%s", dim, len(filtered), reset)
	if len(parts) > 0 {
		fmt.Printf(" %s(%s)%s", dim, strings.Join(parts, ", "), reset)
	}
	fmt.Println()
}

func extractWorktreeName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// ═══════════════════════════════════════════════════════════════════════════
// WORK ITEM TREE - Clean tree with connectors, no box
// ═══════════════════════════════════════════════════════════════════════════

func printWorkItemTree(items []*control.WorkItemInfo) {
	if len(items) == 0 {
		fmt.Println(dim + "No work items found" + reset)
		fmt.Println("Create a goal with: ath goal new \"Description\"")
		return
	}

	// Build parent-children map
	children := make(map[string][]*control.WorkItemInfo)
	roots := make([]*control.WorkItemInfo, 0)

	for _, item := range items {
		if item.ParentID == "" {
			roots = append(roots, item)
		} else {
			children[item.ParentID] = append(children[item.ParentID], item)
		}
	}

	// Stats
	stats := map[string]int{"pending": 0, "in_progress": 0, "completed": 0}

	// Print tree
	for i, root := range roots {
		printTreeNode(root, children, "", i == len(roots)-1, stats)
		if i < len(roots)-1 {
			fmt.Println()
		}
	}

	// Summary
	var parts []string
	if stats["pending"] > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", stats["pending"]))
	}
	if stats["in_progress"] > 0 {
		parts = append(parts, fmt.Sprintf("%d active", stats["in_progress"]))
	}
	if stats["completed"] > 0 {
		parts = append(parts, fmt.Sprintf("%d done", stats["completed"]))
	}

	total := stats["pending"] + stats["in_progress"] + stats["completed"]
	fmt.Printf("\n%s%d items%s", dim, total, reset)
	if len(parts) > 0 {
		fmt.Printf(" %s(%s)%s", dim, strings.Join(parts, ", "), reset)
	}
	fmt.Println()
}

func printTreeNode(item *control.WorkItemInfo, children map[string][]*control.WorkItemInfo, prefix string, isLast bool, stats map[string]int) {
	stats[item.Status]++

	// Shape and color based on type
	var shape, shapeColor string
	switch item.ItemType {
	case "goal":
		shape, shapeColor = shapeGoal, blue
	case "feature":
		shape, shapeColor = shapeFeature, green
	default:
		shape, shapeColor = shapeTask, yellow
	}

	// Fill shape if in_progress
	if item.Status == "in_progress" {
		switch item.ItemType {
		case "goal":
			shape = "■"
		case "feature":
			shape = "◆"
		default:
			shape = "●"
		}
	}

	// Dim everything if completed
	idStyle := dim + cyan // IDs are dimmed cyan
	textStyle := white
	if item.Status == "completed" {
		shapeColor = gray
		idStyle = gray
		textStyle = gray
	}

	// Tree connector
	connector := "├─"
	if isLast {
		connector = "└─"
	}
	if prefix == "" {
		connector = ""
	}

	// Progress indicator
	progressStr := ""
	if item.TotalCount > 0 {
		progressStr = fmt.Sprintf(" %s[%d/%d]%s", gray, item.CompletedCount, item.TotalCount, reset)
	}

	// Ticket
	ticketStr := ""
	if item.TicketID != "" {
		ticketStr = fmt.Sprintf(" %s%s%s", yellow, item.TicketID, reset)
	}

	// Status indicator
	statusStr := ""
	if item.Status == "in_progress" {
		statusStr = fmt.Sprintf(" %s%s active%s", yellow, bullet, reset)
	}

	// Print: connector shape ID subject [progress] [ticket] [status]
	fmt.Printf("%s%s%s%s%s%s %s%s%s %s%s%s%s%s%s\n",
		gray, prefix, connector, reset,
		shapeColor, shape, reset,
		idStyle, item.ID, reset,
		textStyle, item.Subject, reset,
		ticketStr, progressStr+statusStr)

	// Children
	childItems := children[item.ID]
	childPrefix := prefix
	if prefix != "" || len(childItems) > 0 {
		if isLast || prefix == "" {
			childPrefix += "  "
		} else {
			childPrefix += "│ "
		}
	}

	for i, child := range childItems {
		printTreeNode(child, children, childPrefix, i == len(childItems)-1, stats)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// WORK ITEM LIST - Simple list for goal/feat/tsk commands
// ═══════════════════════════════════════════════════════════════════════════

func printWorkItemTable(title string, items []*control.WorkItemInfo) {
	if len(items) == 0 {
		fmt.Println(dim + "No items found" + reset)
		return
	}

	fmt.Printf("%s%s:%s\n", bold, title, reset)

	pendingCount, inProgressCount, completedCount := 0, 0, 0

	for _, item := range items {
		shape := getShapeForType(item.ItemType, item.Status == "in_progress")

		switch item.Status {
		case "in_progress":
			inProgressCount++
		case "completed":
			completedCount++
		default:
			pendingCount++
		}

		progressStr := ""
		if item.TotalCount > 0 {
			progressStr = fmt.Sprintf(" %s[%d/%d]%s", gray, item.CompletedCount, item.TotalCount, reset)
		}

		statusStr := ""
		if item.Status == "in_progress" {
			statusStr = fmt.Sprintf(" %s%s active%s", yellow, bullet, reset)
		}

		// ID dimmed cyan, text white (or gray if completed)
		idStyle, textStyle := dim+cyan, white
		if item.Status == "completed" {
			idStyle, textStyle = gray, gray
		}

		fmt.Printf("  %s %s%s%s %s%s%s%s%s\n",
			shape,
			idStyle, item.ID, reset,
			textStyle, item.Subject, reset,
			progressStr, statusStr)
	}

	// Summary
	var parts []string
	if pendingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pendingCount))
	}
	if inProgressCount > 0 {
		parts = append(parts, fmt.Sprintf("%d active", inProgressCount))
	}
	if completedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d done", completedCount))
	}

	fmt.Printf("\n%s%d items%s", dim, len(items), reset)
	if len(parts) > 0 {
		fmt.Printf(" %s(%s)%s", dim, strings.Join(parts, ", "), reset)
	}
	fmt.Println()
}

// ═══════════════════════════════════════════════════════════════════════════
// STATUS VIEW - Active work and ready items
// ═══════════════════════════════════════════════════════════════════════════

func printStatusBox(inProgress, ready []*control.WorkItemInfo) {
	if len(inProgress) == 0 && len(ready) == 0 {
		fmt.Println(dim + "No active work items" + reset)
		fmt.Println(dim + "Use 'ath goal new' or 'ath tsk' to create work items" + reset)
		return
	}

	if len(inProgress) > 0 {
		fmt.Printf("%sActive:%s\n", bold, reset)
		for _, item := range inProgress {
			shape := getShapeForType(item.ItemType, true)
			fmt.Printf("  %s %s%s%s %s%s%s\n",
				shape,
				dim+cyan, item.ID, reset,
				white, item.Subject, reset)
			if item.AgentID != "" {
				fmt.Printf("    %sAgent: %s%s%s\n", gray, yellow, item.AgentID, reset)
			}
		}
		fmt.Println()
	}

	if len(ready) > 0 {
		fmt.Printf("%sReady (%d):%s\n", bold, len(ready), reset)
		maxShow := 10
		for i, item := range ready {
			if i >= maxShow {
				fmt.Printf("  %s... and %d more%s\n", dim, len(ready)-maxShow, reset)
				break
			}
			shape := getShapeForType(item.ItemType, false)
			fmt.Printf("  %s %s%s%s %s%s%s\n",
				shape,
				dim+cyan, item.ID, reset,
				gray, item.Subject, reset)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// SHARED HELPERS
// ═══════════════════════════════════════════════════════════════════════════

func getShapeForType(itemType string, filled bool) string {
	if filled {
		switch itemType {
		case "goal":
			return blue + "■" + reset
		case "feature":
			return green + "◆" + reset
		default:
			return yellow + "●" + reset
		}
	}
	switch itemType {
	case "goal":
		return blue + "□" + reset
	case "feature":
		return green + "◇" + reset
	default:
		return yellow + "○" + reset
	}
}

func printSuccess(msg string) {
	fmt.Printf("%s%s %s%s\n", green, checkMark, msg, reset)
}

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%sError: %s%s\n", red, msg, reset)
}

// Legacy compatibility
func printWorkItem(item *control.WorkItemInfo, indent int) {
	shape := getShapeForType(item.ItemType, item.Status == "in_progress")
	indentStr := strings.Repeat("  ", indent)
	fmt.Printf("%s%s %s%s%s  %s\n", indentStr, shape, dim, item.ID, reset, item.Subject)
}
