package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
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
// WORKTREE TABLE - lipgloss table layout
// ═══════════════════════════════════════════════════════════════════════════

func extractWorktreeName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// isTicketPrefix checks if a string looks like a ticket prefix (2-5 uppercase letters)
func isTicketPrefix(s string) bool {
	if len(s) < 2 || len(s) > 5 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	return true
}

// isNumeric checks if a string starts with digits (allowing suffixes like "123-something")
func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// worktreeID extracts a display ID from a worktree.
func worktreeID(wt *control.WorktreeInfo, maxWidth int) string {
	id := wt.TicketID
	if id == "" {
		name := extractWorktreeName(wt.Path)
		if strings.HasPrefix(name, "wi-") || strings.HasPrefix(name, "WI-") {
			id = name
		} else {
			parts := strings.SplitN(name, "-", 2)
			if len(parts) >= 2 && isTicketPrefix(parts[0]) && isNumeric(parts[1]) {
				id = strings.ToUpper(parts[0]) + "-" + parts[1]
			} else {
				id = truncate(name, maxWidth)
			}
		}
	}
	return truncate(id, maxWidth)
}

// worktreeStatus builds a status string with ahead/behind and git status indicators.
func worktreeStatus(wt *control.WorktreeInfo, ab AheadBehind, hasAB bool, stats *[4]int) string {
	var parts []string

	if hasAB {
		if ab.Behind > 0 {
			parts = append(parts, fmt.Sprintf("%s↓%d%s", red, ab.Behind, reset))
		}
		if ab.Ahead > 0 {
			parts = append(parts, fmt.Sprintf("%s↑%d%s", cyan, ab.Ahead, reset))
		}
	}

	switch {
	case wt.Status == "" || wt.Status == "clean":
		parts = append(parts, green+checkMark+reset)
		stats[0]++ // clean
	case strings.Contains(wt.Status, "untracked"):
		parts = append(parts, yellow+"?"+reset)
		stats[1]++ // untracked
	default:
		parts = append(parts, yellow+bullet+reset)
		stats[2]++ // changes
	}

	return strings.Join(parts, " ")
}

// printWorktreeTableWithQueue shows worktrees with queue position and ahead/behind indicators.
// Queued worktrees are sorted to the top.
func printWorktreeTableWithQueue(worktrees []*control.WorktreeInfo, queuePositions map[string]int, aheadBehind map[string]AheadBehind) {
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

	// Sort: queued items first (by position), then alphabetical
	sort.Slice(filtered, func(i, j int) bool {
		posI, inQueueI := queuePositions[filtered[i].Path]
		posJ, inQueueJ := queuePositions[filtered[j].Path]
		if inQueueI && inQueueJ {
			return posI < posJ
		}
		if inQueueI {
			return true
		}
		if inQueueJ {
			return false
		}
		return extractWorktreeName(filtered[i].Path) < extractWorktreeName(filtered[j].Path)
	})

	// Build rows; stats: [clean, untracked, changes, queued]
	var stats [4]int
	var rows [][]string
	for _, wt := range filtered {
		id := worktreeID(wt, 14)
		branch := truncate(wt.Branch, 45)

		queueStr := ""
		if pos, ok := queuePositions[wt.Path]; ok {
			queueStr = cyan + fmt.Sprintf("#%d", pos) + reset
			stats[3]++
		}

		ab, hasAB := aheadBehind[wt.Path]
		status := worktreeStatus(wt, ab, hasAB, &stats)

		rows = append(rows, []string{queueStr, dim + magenta + id + reset, cyan + branch + reset, status})
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cellStyle := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	headerStyle := cellStyle.Foreground(lipgloss.Color("240"))

	t := table.New().
		Headers("Q", "ID", "BRANCH", "STATUS").
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		BorderColumn(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})

	for _, row := range rows {
		t.Row(row...)
	}

	fmt.Println(t.Render())

	// Summary
	var parts []string
	if stats[3] > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", stats[3]))
	}
	if stats[0] > 0 {
		parts = append(parts, fmt.Sprintf("%d clean", stats[0]))
	}
	if stats[2] > 0 {
		parts = append(parts, fmt.Sprintf("%d with changes", stats[2]))
	}
	if stats[1] > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", stats[1]))
	}

	fmt.Printf("%s%d worktrees%s", dim, len(filtered), reset)
	if len(parts) > 0 {
		fmt.Printf(" %s(%s)%s", dim, strings.Join(parts, ", "), reset)
	}
	fmt.Println()
}

// ═══════════════════════════════════════════════════════════════════════════
// AGENT TABLE - Boxed table showing agents with session IDs
// ═══════════════════════════════════════════════════════════════════════════

func printAgentTable(agents []*control.AgentInfo) {
	if len(agents) == 0 {
		fmt.Println(dim + "No agents found" + reset)
		fmt.Println(dim + "Spawn one with: ath spawn -f <feature-id> --headless" + reset)
		return
	}

	// Column widths
	const idWidth = 8
	const statusWidth = 12
	const archetypeWidth = 10
	const worktreeWidth = 22
	const sessionWidth = 10
	const innerWidth = 1 + idWidth + 2 + statusWidth + 2 + archetypeWidth + 2 + worktreeWidth + 2 + sessionWidth + 1

	// Header
	titleText := "Agents"
	rightDashes := innerWidth - 2 - len(titleText) - 1
	fmt.Printf("%s%s%s %s%s%s %s%s\n",
		gray, boxTopLeft, boxHorizontal,
		dim+cyan, titleText, reset,
		gray+strings.Repeat(boxHorizontal, rightDashes)+boxTopRight, reset)

	// Column headers
	fmt.Printf("%s%s%s %s%s  %s  %s  %s  %s %s%s%s\n",
		gray, boxVertical, reset,
		dim, padRight("ID", idWidth),
		padRight("STATUS", statusWidth),
		padRight("TYPE", archetypeWidth),
		padRight("WORKTREE", worktreeWidth),
		padRight("SESSION", sessionWidth),
		reset+gray, boxVertical, reset)

	// Separator
	fmt.Printf("%s%s%s%s%s\n",
		gray, boxTeeRight,
		strings.Repeat(boxHorizontal, innerWidth),
		boxTeeLeft, reset)

	// Stats
	runningCount, completedCount, crashedCount := 0, 0, 0

	for _, a := range agents {
		shortID := a.ID
		if len(shortID) > idWidth {
			shortID = shortID[:idWidth]
		}

		// Status with color
		statusColor := gray
		switch a.Status {
		case "running", "planning", "executing", "spawning":
			statusColor = green
			runningCount++
		case "completed":
			completedCount++
		case "crashed":
			statusColor = red
			crashedCount++
		case "awaiting", "interactive":
			statusColor = yellow
			runningCount++
		case "terminated":
			completedCount++
		default:
			completedCount++
		}

		archetype := truncate(a.Archetype, archetypeWidth)
		wtName := truncate(extractWorktreeName(a.WorktreePath), worktreeWidth)

		shortSession := ""
		if a.ClaudeSessionID != "" {
			shortSession = a.ClaudeSessionID
			if len(shortSession) > sessionWidth {
				shortSession = shortSession[:sessionWidth]
			}
		}

		fmt.Printf("%s%s%s %s%s%s  %s%s%s  %s%s%s  %s%s%s  %s%s%s %s%s%s\n",
			gray, boxVertical, reset,
			magenta, padRight(shortID, idWidth), reset,
			statusColor, padRight(a.Status, statusWidth), reset,
			dim, padRight(archetype, archetypeWidth), reset,
			cyan, padRight(wtName, worktreeWidth), reset,
			yellow, padRight(shortSession, sessionWidth), reset,
			gray, boxVertical, reset)
	}

	// Footer
	fmt.Printf("%s%s%s%s%s\n",
		gray, boxBottomLeft,
		strings.Repeat(boxHorizontal, innerWidth),
		boxBottomRight, reset)

	// Summary
	var parts []string
	if runningCount > 0 {
		parts = append(parts, fmt.Sprintf("%d running", runningCount))
	}
	if completedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completedCount))
	}
	if crashedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d crashed", crashedCount))
	}

	fmt.Printf("\n%s%d agents%s", dim, len(agents), reset)
	if len(parts) > 0 {
		fmt.Printf(" %s(%s)%s", dim, strings.Join(parts, ", "), reset)
	}
	fmt.Println()
}

func printAgentDetail(a *control.AgentInfo) {
	shortID := a.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	// Status color
	statusColor := gray
	switch a.Status {
	case "running", "planning", "executing", "spawning":
		statusColor = green
	case "crashed":
		statusColor = red
	case "awaiting", "interactive":
		statusColor = yellow
	}

	fmt.Printf("%sAgent %s%s%s\n\n", bold, magenta, shortID, reset)
	fmt.Printf("  %sID:%s         %s\n", dim, reset, a.ID)
	fmt.Printf("  %sStatus:%s     %s%s%s\n", dim, reset, statusColor, a.Status, reset)
	fmt.Printf("  %sArchetype:%s  %s\n", dim, reset, a.Archetype)
	fmt.Printf("  %sProject:%s    %s\n", dim, reset, a.ProjectName)
	fmt.Printf("  %sWorktree:%s   %s%s%s\n", dim, reset, cyan, a.WorktreePath, reset)
	fmt.Printf("  %sCreated:%s    %s\n", dim, reset, a.CreatedAt)

	if a.ClaudeSessionID != "" {
		fmt.Printf("  %sSession:%s    %s%s%s\n", dim, reset, yellow, a.ClaudeSessionID, reset)
	}
	if a.TaskListID != "" {
		fmt.Printf("  %sTask list:%s  %s\n", dim, reset, a.TaskListID)
	}
	if a.PlanStatus != "" {
		planInfo := a.PlanStatus
		if a.PlanPath != "" {
			planInfo += fmt.Sprintf(" %s(%s)%s", dim, a.PlanPath, reset)
		}
		fmt.Printf("  %sPlan:%s       %s\n", dim, reset, planInfo)
	} else if a.PlanPath != "" {
		fmt.Printf("  %sPlan:%s       %s\n", dim, reset, a.PlanPath)
	}
	if a.RestartCount > 0 {
		fmt.Printf("  %sRestarts:%s   %d\n", dim, reset, a.RestartCount)
	}

	// Metrics
	if a.Metrics != nil {
		fmt.Println()
		fmt.Printf("  %sMetrics:%s\n", bold, reset)
		if a.Metrics.NumTurns > 0 {
			fmt.Printf("    Turns:      %d\n", a.Metrics.NumTurns)
		}
		if a.Metrics.ToolUseCount > 0 {
			fmt.Printf("    Tool calls: %d\n", a.Metrics.ToolUseCount)
		}
		if a.Metrics.FilesRead > 0 || a.Metrics.FilesWritten > 0 {
			fmt.Printf("    Files:      %d read, %d written\n", a.Metrics.FilesRead, a.Metrics.FilesWritten)
		}
		if a.Metrics.InputTokens > 0 {
			fmt.Printf("    Tokens:     %dk in, %dk out", a.Metrics.InputTokens/1000, a.Metrics.OutputTokens/1000)
			if a.Metrics.CacheHitRate > 0 {
				fmt.Printf(" (%.0f%% cache hit)", a.Metrics.CacheHitRate)
			}
			fmt.Println()
		}
		if a.Metrics.CostCents > 0 {
			fmt.Printf("    Cost:       $%.2f\n", float64(a.Metrics.CostCents)/100)
		}
		if a.Metrics.DurationMs > 0 {
			secs := a.Metrics.DurationMs / 1000
			if secs < 60 {
				fmt.Printf("    Duration:   %ds\n", secs)
			} else {
				fmt.Printf("    Duration:   %dm%ds\n", secs/60, secs%60)
			}
		}
	}

	// Resume hint
	if a.ClaudeSessionID != "" {
		fmt.Println()
		isActive := a.Status == "running" || a.Status == "planning" || a.Status == "executing" || a.Status == "awaiting" || a.Status == "interactive"
		if isActive {
			fmt.Printf("  %sResume interactively:%s\n", dim, reset)
		} else {
			fmt.Printf("  %sContinue this session:%s\n", dim, reset)
		}
		fmt.Printf("    claude --resume %s\n", a.ClaudeSessionID)
	}
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
	isBlocked := item.Blocked && item.Status != "completed"
	stats[item.Status]++

	// Shape based on type and status
	var shape string
	switch item.ItemType {
	case "goal":
		shape = shapeGoal
	case "feature":
		shape = shapeFeature
	default:
		shape = shapeTask
	}

	// Fill shape if in_progress or completed
	if item.Status == "in_progress" || item.Status == "completed" {
		switch item.ItemType {
		case "goal":
			shape = shapeGoalFilled
		case "feature":
			shape = shapeFeatureFilled
		default:
			shape = shapeTaskFilled
		}
	}
	// Blocked tasks use open shapes (override filled for in_progress+blocked)
	if isBlocked {
		switch item.ItemType {
		case "goal":
			shape = shapeGoal
		case "feature":
			shape = shapeFeature
		default:
			shape = shapeTask
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

	// Blocked items are red
	if isBlocked {
		shapeColor = red
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
	if item.Status == "in_progress" && !isBlocked {
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

	// Add helpful hints
	fmt.Println()
	fmt.Printf("%sRun 'ath --help' to see all commands%s\n", dim, reset)
}

// ═══════════════════════════════════════════════════════════════════════════
// SHARED HELPERS
// ═══════════════════════════════════════════════════════════════════════════

// getShapeForItem returns colored shape based on item type and status.
// Shape: open=pending/blocked, filled=in_progress/completed
// Colors: goal=blue, feature=green, task=yellow, completed=gray, blocked=red
func getShapeForItem(item *control.WorkItemInfo) string {
	isBlocked := item.Blocked && item.Status != "completed"

	// Determine shape: filled for in_progress and completed, open for pending and blocked
	var shape string
	if (item.Status == "in_progress" || item.Status == "completed") && !isBlocked {
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

	// Color by state
	var color string
	if item.Status == "completed" {
		color = gray
	} else if isBlocked {
		color = red
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

// ═══════════════════════════════════════════════════════════════════════════
// QUEUE GRAPH - Visual pipeline from main through queue items
// ═══════════════════════════════════════════════════════════════════════════

func printQueueGraph(items []*control.MergeQueueItemInfo, baseBranch string) {
	if baseBranch == "" {
		baseBranch = "main"
	}

	if len(items) == 0 {
		fmt.Printf("  %s●%s %s%s%s\n", green, reset, bold, baseBranch, reset)
		fmt.Println()
		fmt.Println(dim + "  Queue empty - new worktrees branch from " + baseBranch + reset)
		return
	}

	// Header
	fmt.Printf("  %s≡%s %sMerge Queue%s %s(%d)%s\n\n",
		cyan, reset, bold, reset, dim, len(items), reset)

	// Root node: main
	fmt.Printf("  %s●%s %s%s%s\n", green, reset, bold, baseBranch, reset)

	for i, item := range items {
		isLast := i == len(items)-1
		branchShort := item.Branch
		statusIcon := queueGraphStatusIcon(item.Status)

		// Connector line
		fmt.Printf("  %s│%s\n", gray, reset)

		// Node connector
		connector := "├"
		if isLast {
			connector = "└"
		}

		// Position + branch
		fmt.Printf("  %s%s── %s%s#%d%s %s%s%s  %s\n",
			gray, connector, reset,
			yellow, item.Position, reset,
			cyan, branchShort, reset,
			statusIcon)

		// Path (dimmed, indented)
		indent := "│"
		if isLast {
			indent = " "
		}
		displayPath := item.WorktreePath
		if len(displayPath) > 50 {
			displayPath = "..." + displayPath[len(displayPath)-47:]
		}
		fmt.Printf("  %s%s%s       %s%s%s\n",
			gray, indent, reset,
			dim, displayPath, reset)
	}

	fmt.Println()

	// Legend
	var statusCounts = map[string]int{}
	for _, item := range items {
		statusCounts[item.Status]++
	}

	var parts []string
	for status, count := range statusCounts {
		parts = append(parts, fmt.Sprintf("%d %s", count, status))
	}
	fmt.Printf("  %s%s%s\n", dim, strings.Join(parts, ", "), reset)
}

func queueGraphStatusIcon(status string) string {
	switch status {
	case "queued":
		return cyan + "queued" + reset
	case "merging":
		return green + bold + "merging" + reset
	case "merged":
		return green + "merged" + reset
	case "rebasing":
		return yellow + "rebasing" + reset
	case "diverged":
		return yellow + "diverged" + reset
	case "conflict":
		return red + bold + "conflict" + reset
	default:
		return dim + status + reset
	}
}

func printSuccess(msg string) {
	fmt.Printf("%s%s %s%s\n", green, checkMark, msg, reset)
}

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%sError: %s%s\n", red, msg, reset)
}

// ═══════════════════════════════════════════════════════════════════════════
// RECONCILE RESULTS - Per-item rebase outcome
// ═══════════════════════════════════════════════════════════════════════════

func printReconcileResults(results []map[string]string) {
	if len(results) == 0 {
		fmt.Println(dim + "Nothing to reconcile - queue is clean" + reset)
		return
	}

	fmt.Printf("%s≡%s Reconcile Results (%d items)\n\n", cyan, reset, len(results))

	successCount, conflictCount, errorCount := 0, 0, 0

	for _, r := range results {
		branch := r["branch"]
		status := r["status"]

		var statusIcon, statusColor string
		switch status {
		case "success":
			statusIcon = green + checkMark + reset
			statusColor = green
			successCount++
		case "conflict":
			statusIcon = yellow + "!" + reset
			statusColor = yellow
			conflictCount++
		default:
			statusIcon = red + "x" + reset
			statusColor = red
			errorCount++
		}

		fmt.Printf("  %s %s%s%s %s\n", statusIcon, statusColor, status, reset, branch)

		if head, ok := r["head"]; ok && head != "" {
			fmt.Printf("      %sHEAD:%s %s\n", gray, reset, shortSHA(head))
		}
		if base, ok := r["base"]; ok && base != "" {
			fmt.Printf("      %sbase:%s %s\n", gray, reset, base)
		}
		if errMsg, ok := r["error"]; ok && errMsg != "" {
			fmt.Printf("      %serror:%s %s\n", red, reset, truncate(errMsg, 80))
		}
	}

	fmt.Println()

	var parts []string
	if successCount > 0 {
		parts = append(parts, fmt.Sprintf("%d rebased", successCount))
	}
	if conflictCount > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", conflictCount))
	}
	if errorCount > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errorCount))
	}
	fmt.Printf("%s%s%s\n", dim, strings.Join(parts, ", "), reset)
}

// Legacy compatibility
func printWorkItem(item *control.WorkItemInfo, indent int) {
	shape := getShapeForItem(item)
	indentStr := strings.Repeat("  ", indent)
	fmt.Printf("%s%s %s%s%s  %s%s%s\n", indentStr, shape, magenta, item.ID, reset, white, item.Subject, reset)
}
