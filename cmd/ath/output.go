package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/drewfead/athena/internal/control"
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

	// Align IDs
	idWidth := 0
	for _, item := range items {
		if len(item.ID) > idWidth {
			idWidth = len(item.ID)
		}
	}

	// Print tree
	for i, root := range roots {
		printTreeNode(root, children, "", i == len(roots)-1, stats, idWidth)
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

func printTreeNode(item *control.WorkItemInfo, children map[string][]*control.WorkItemInfo, prefix string, isLast bool, stats map[string]int, idWidth int) {
	stats[item.Status]++

	// Shape based on type
	var shape string
	switch item.ItemType {
	case "goal":
		shape = shapeGoal
	case "feature":
		shape = shapeFeature
	default:
		shape = shapeTask
	}

	// Fill shape if in_progress
	if item.Status == "in_progress" {
		switch item.ItemType {
		case "goal":
			shape = shapeGoalFilled
		case "feature":
			shape = shapeFeatureFilled
		default:
			shape = shapeTaskFilled
		}
	}

	// Color shape by item type: goal=blue, feature=green, task=yellow
	var shapeColor string
	switch item.ItemType {
	case "goal":
		shapeColor = blue
	case "feature":
		shapeColor = green
	default: // task
		shapeColor = yellow
	}

	// IDs are magenta, text is white
	idStyle := magenta
	textStyle := white

	// Dim everything if completed
	if item.Status == "completed" {
		shapeColor = gray
		idStyle = gray
		textStyle = gray
	}

	// Tree connector
	connector := treeBranch
	if isLast {
		connector = treeLastBranch
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
	paddedID := padRight(item.ID, idWidth)
	// Sanitize subject: replace newlines with spaces, collapse multiple spaces, truncate
	subject := strings.ReplaceAll(item.Subject, "\n", " ")
	subject = strings.ReplaceAll(subject, "\r", "")
	subject = strings.Join(strings.Fields(subject), " ")
	subject = truncate(subject, 80) // Limit width to prevent wrapping
	fmt.Printf("%s%s%s%s%s%s %s%s%s %s%s%s%s%s%s\n",
		gray, prefix, connector, reset,
		shapeColor, shape, reset,
		idStyle, paddedID, reset,
		textStyle, subject, reset,
		ticketStr, progressStr+statusStr)

	// Children
	childItems := children[item.ID]
	childPrefix := prefix
	if prefix != "" || len(childItems) > 0 {
		if isLast || prefix == "" {
			childPrefix += treeSpace
		} else {
			childPrefix += treeVertical
		}
	}

	for i, child := range childItems {
		printTreeNode(child, children, childPrefix, i == len(childItems)-1, stats, idWidth)
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
		shape := getShapeForItem(item)

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

		// ID magenta, text white (or gray if completed)
		idStyle, textStyle := magenta, white
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
			shape := getShapeForItem(item)
			fmt.Printf("  %s %s%s%s %s%s%s\n",
				shape,
				magenta, item.ID, reset,
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
			shape := getShapeForItem(item)
			fmt.Printf("  %s %s%s%s %s%s%s\n",
				shape,
				magenta, item.ID, reset,
				white, item.Subject, reset)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// SHARED HELPERS
// ═══════════════════════════════════════════════════════════════════════════

// getShapeForItem returns colored shape based on item type and priority
// getShapeForItem returns colored shape based on item type
// Colors: goal=blue, feature=green, task=yellow (gray if completed)
func getShapeForItem(item *control.WorkItemInfo) string {
	var shape string
	if item.Status == "in_progress" {
		switch item.ItemType {
		case "goal":
			shape = shapeGoalFilled
		case "feature":
			shape = shapeFeatureFilled
		default:
			shape = shapeTaskFilled
		}
	} else {
		switch item.ItemType {
		case "goal":
			shape = shapeGoal
		case "feature":
			shape = shapeFeature
		default:
			shape = shapeTask
		}
	}

	// Color by item type (dimmed if completed)
	var color string
	if item.Status == "completed" {
		color = gray
	} else {
		switch item.ItemType {
		case "goal":
			color = blue
		case "feature":
			color = green
		default: // task
			color = yellow
		}
	}

	return color + shape + reset
}

func printSuccess(msg string) {
	fmt.Printf("%s%s %s%s\n", green, checkMark, msg, reset)
}

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%sError: %s%s\n", red, msg, reset)
}

// Legacy compatibility
func printWorkItem(item *control.WorkItemInfo, indent int) {
	shape := getShapeForItem(item)
	indentStr := strings.Repeat("  ", indent)
	fmt.Printf("%s%s %s%s%s  %s%s%s\n", indentStr, shape, magenta, item.ID, reset, white, item.Subject, reset)
}
