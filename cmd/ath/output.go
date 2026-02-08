package main

import (
	"fmt"
	"os"
	"sort"
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

// isTicketPrefix checks if a string looks like a ticket prefix (2-5 uppercase letters)
func isTicketPrefix(s string) bool {
	if len(s) < 2 || len(s) > 5 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			// Allow lowercase too, we'll uppercase it
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
	// Just check first char is a digit
	return s[0] >= '0' && s[0] <= '9'
}

// printWorktreeTableWithQueue shows worktrees with queue position and ahead/behind indicators.
// Queued worktrees are sorted to the top.
func printWorktreeTableWithQueue(worktrees []*control.WorktreeInfo, queuePositions map[string]int, aheadBehind map[string]AheadBehind) {
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

	// Sort: queued items first (by position), then others
	sort.Slice(filtered, func(i, j int) bool {
		posI, inQueueI := queuePositions[filtered[i].Path]
		posJ, inQueueJ := queuePositions[filtered[j].Path]

		// Both in queue: sort by position
		if inQueueI && inQueueJ {
			return posI < posJ
		}
		// Only i in queue: i comes first
		if inQueueI {
			return true
		}
		// Only j in queue: j comes first
		if inQueueJ {
			return false
		}
		// Neither in queue: alphabetical by name
		return extractWorktreeName(filtered[i].Path) < extractWorktreeName(filtered[j].Path)
	})

	// Column widths: Q | ID | BRANCH (summary) | STATUS
	const queueWidth = 3
	const idWidth = 14
	const branchWidth = 45
	const statusWidth = 12
	const innerWidth = 1 + queueWidth + 1 + idWidth + 2 + branchWidth + 2 + statusWidth + 1

	// Header
	titleText := "Worktrees"
	rightDashes := innerWidth - 2 - len(titleText) - 1
	fmt.Printf("%s%s%s %s%s%s %s%s\n",
		gray, boxTopLeft, boxHorizontal,
		dim+cyan, titleText, reset,
		gray+strings.Repeat(boxHorizontal, rightDashes)+boxTopRight, reset)

	// Column headers
	fmt.Printf("%s%s%s %s%s %s  %s  %s%s %s%s%s\n",
		gray, boxVertical, reset,
		dim, padRight("Q", queueWidth),
		padRight("ID", idWidth),
		padRight("BRANCH", branchWidth),
		padRight("STATUS", statusWidth), reset,
		gray, boxVertical, reset)

	// Separator
	fmt.Printf("%s%s%s%s%s\n",
		gray, boxTeeRight,
		strings.Repeat(boxHorizontal, innerWidth),
		boxTeeLeft, reset)

	// Stats
	cleanCount, changesCount, untrackedCount, queuedCount := 0, 0, 0, 0

	// Rows
	for _, wt := range filtered {
		// ID: prefer TicketID, then wi-id pattern, then ticket pattern, then short name
		id := wt.TicketID
		if id == "" {
			name := extractWorktreeName(wt.Path)
			// Check for wi-xxxx pattern (work item ID)
			if strings.HasPrefix(name, "wi-") || strings.HasPrefix(name, "WI-") {
				id = name
			} else {
				// Check for real ticket pattern: 2-4 uppercase letters + dash + numbers
				// e.g., ENG-123, OMI-42, ATH-1 (not athena-cli-fix)
				parts := strings.SplitN(name, "-", 2)
				if len(parts) >= 2 && isTicketPrefix(parts[0]) && isNumeric(parts[1]) {
					id = strings.ToUpper(parts[0]) + "-" + parts[1]
				} else {
					// Just use the short directory name
					id = truncate(name, idWidth)
				}
			}
		}
		id = truncate(id, idWidth)
		branch := truncate(wt.Branch, branchWidth)

		// Queue position
		var queueStr string
		if pos, ok := queuePositions[wt.Path]; ok {
			queueStr = fmt.Sprintf("#%d", pos)
			queuedCount++
		}

		// Status with ahead/behind indicators
		var statusParts []string

		// Ahead/behind arrows
		if ab, ok := aheadBehind[wt.Path]; ok {
			if ab.Behind > 0 {
				statusParts = append(statusParts, fmt.Sprintf("%s↓%d%s", red, ab.Behind, reset))
			}
			if ab.Ahead > 0 {
				statusParts = append(statusParts, fmt.Sprintf("%s↑%d%s", cyan, ab.Ahead, reset))
			}
		}

		// Git status
		switch {
		case wt.Status == "" || wt.Status == "clean":
			statusParts = append(statusParts, green+checkMark+reset)
			cleanCount++
		case strings.Contains(wt.Status, "untracked"):
			statusParts = append(statusParts, yellow+"?"+reset)
			untrackedCount++
		default:
			statusParts = append(statusParts, yellow+bullet+reset)
			changesCount++
		}

		plainStatus := strings.Join(statusParts, " ")

		// Print row
		queueColor := ""
		if queueStr != "" {
			queueColor = cyan
		}
		fmt.Printf("%s%s%s %s%s%s %s%s%s  %s%s%s  %s %s%s%s\n",
			gray, boxVertical, reset,
			queueColor, padRight(queueStr, queueWidth), reset,
			dim+magenta, padRight(id, idWidth), reset,
			cyan, padRight(branch, branchWidth), reset,
			plainStatus,
			gray, boxVertical, reset)
	}

	// Footer
	fmt.Printf("%s%s%s%s%s\n",
		gray, boxBottomLeft,
		strings.Repeat(boxHorizontal, innerWidth),
		boxBottomRight, reset)

	// Summary
	var parts []string
	if queuedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", queuedCount))
	}
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
	if a.PlanStatus != "" {
		fmt.Printf("  %sPlan:%s       %s\n", dim, reset, a.PlanStatus)
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

// Legacy compatibility
func printWorkItem(item *control.WorkItemInfo, indent int) {
	shape := getShapeForItem(item)
	indentStr := strings.Repeat("  ", indent)
	fmt.Printf("%s%s %s%s%s  %s%s%s\n", indentStr, shape, magenta, item.ID, reset, white, item.Subject, reset)
}
