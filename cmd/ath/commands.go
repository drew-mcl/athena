package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/drewfead/athena/internal/cli"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/plugin"
)

// ============================================================================
// Spawn Command
// ============================================================================

// isTicketID detects ticket-like IDs: letters + hyphen + numbers (e.g. ENG-123, PROJ-45)
func isTicketID(id string) bool {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return false
	}
	prefix, number := parts[0], parts[1]
	if len(prefix) < 2 || len(prefix) > 10 {
		return false
	}
	// Prefix must be all letters
	for _, c := range prefix {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	// Number part must start with a digit
	if len(number) == 0 || number[0] < '0' || number[0] > '9' {
		return false
	}
	return true
}

// isWorkItemID detects work item IDs: starts with "wi-" (e.g. wi-a3f8, wi-a3f8.1)
func isWorkItemID(id string) bool {
	return strings.HasPrefix(id, "wi-")
}

// selectArchetype shows an interactive menu to select an agent archetype.
func selectArchetype() (string, error) {
	type archetypeOption struct {
		name        string
		description string
		source      string // "built-in" or "installed"
	}

	var options []archetypeOption

	// Built-in archetypes (from internal/config/config.go)
	builtIn := []archetypeOption{
		{"orchestrator", "Goal-level coordination and feature breakdown", "built-in"},
		{"executor", "Feature implementation and commits", "built-in"},
		{"planner", "Exploration and planning without changes", "built-in"},
		{"reviewer", "Code review and quality checks", "built-in"},
		{"brainstorm", "Interactive ideation and exploration", "built-in"},
		{"reconciler", "Branch cleanup, queue reconciliation", "built-in"},
		{"mapper", "Codebase documentation and mapping", "built-in"},
	}
	options = append(options, builtIn...)

	// Installed archetypes (from .claude/agents/)
	root, err := detectProjectRoot()
	if err == nil {
		agentsDir := filepath.Join(root, ".claude", "agents")
		entries, err := os.ReadDir(agentsDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
					name := strings.TrimSuffix(entry.Name(), ".md")
					// Simple descriptions - could parse from frontmatter if needed
					desc := "Custom agent archetype"
					options = append(options, archetypeOption{name, desc, "installed"})
				}
			}
		}
	}

	if len(options) == 0 {
		return "", fmt.Errorf("no archetypes available")
	}

	// Display menu
	fmt.Printf("\n%sSelect Agent Archetype:%s\n\n", bold, reset)
	for i, opt := range options {
		source := gray + opt.source + reset
		if opt.source == "built-in" {
			source = dim + opt.source + reset
		}
		fmt.Printf("  %s%2d%s. %s%-20s%s %s[%s]%s\n",
			cyan, i+1, reset,
			bold, opt.name, reset,
			opt.description,
			gray, source, reset)
	}
	fmt.Printf("\n%sEnter number (or 0 to cancel):%s ", dim, reset)

	var choice int
	_, err = fmt.Scanln(&choice)
	if err != nil || choice <= 0 || choice > len(options) {
		return "", fmt.Errorf("invalid selection")
	}

	selected := options[choice-1].name
	fmt.Printf("%s%s%s Selected: %s%s%s\n\n", green, checkMark, reset, magenta, selected, reset)
	return selected, nil
}

func runSpawn(featureID, id, archetype string, retrieve, headless, worktree, parallel bool) error {
	// If archetype is "select" or empty with interactive selection requested, prompt user
	if archetype == "select" || archetype == "?" {
		selected, err := selectArchetype()
		if err != nil {
			return fmt.Errorf("archetype selection cancelled or failed: %w", err)
		}
		archetype = selected
	}

	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	req := control.SpawnRequest{
		Project:   project,
		Retrieve:  retrieve,
		Headless:  headless,
		Worktree:  worktree,
		Parallel:  parallel,
		WorkDir:   cwd,
		Archetype: archetype,
	}

	// Feature flag takes priority
	if featureID != "" {
		req.FeatureID = featureID
	} else if id == "" && worktree {
		// If -w flag is set but no feature ID provided, try context
		ctx, ctxErr := cli.LoadContext()
		if ctxErr == nil && ctx.LastFeatureID != "" {
			featureID = ctx.LastFeatureID
			req.FeatureID = featureID
			fmt.Printf("%sUsing feature from context: %s%s\n", dim, featureID, reset)
		}
	}

	if id != "" && featureID == "" {
		// Classify the positional ID argument
		if isWorkItemID(id) {
			req.WorkItemID = id
		} else if isTicketID(id) {
			req.TicketID = strings.ToUpper(id)
		} else {
			return fmt.Errorf("invalid id %q: expected a work item ID (wi-xxxx) or ticket ID (ENG-123)\nHint: use 'ath tree' to find work item IDs", id)
		}
	}

	resp, err := client.Spawn(req)
	if err != nil {
		return err
	}

	// Print context summary
	if headless && resp.Agent != nil {
		agentID := resp.Agent.ID
		if len(agentID) > 8 {
			agentID = agentID[:8]
		}
		fmt.Printf("%s%s%s Spawned agent %s%s%s\n", green, checkMark, reset, magenta, agentID, reset)
	} else {
		fmt.Printf("%s%s%s Launching Claude Code\n", green, checkMark, reset)
	}
	fmt.Printf("  Work item: %s%s%s  %s\n", magenta, resp.WorkItem.ID, reset, resp.WorkItem.Subject)
	fmt.Printf("  Task list: %s%s%s\n", gray, resp.TaskListID, reset)
	if resp.Worktree != nil {
		fmt.Printf("  Worktree:  %s%s%s\n", cyan, resp.Worktree.Path, reset)
	}
	if headless && resp.Agent != nil && resp.Agent.ClaudeSessionID != "" {
		fmt.Printf("  Session:   %s%s%s\n", yellow, resp.Agent.ClaudeSessionID, reset)
	}
	fmt.Println()

	if headless {
		if resp.Agent != nil {
			fmt.Printf("%sMonitor:  ath agent %s%s\n", dim, resp.Agent.ID[:8], reset)
			if resp.Agent.ClaudeSessionID != "" {
				fmt.Printf("%sResume:   claude --resume %s%s\n", dim, resp.Agent.ClaudeSessionID, reset)
			}
		}
		return nil
	}

	if len(resp.ExecArgs) == 0 {
		return fmt.Errorf("daemon returned no exec args for interactive mode")
	}

	// Find claude binary
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	// If we have a worktree, chdir into it before exec
	if resp.Worktree != nil && resp.Worktree.Path != "" {
		if err := os.Chdir(resp.Worktree.Path); err != nil {
			return fmt.Errorf("failed to chdir to worktree: %w", err)
		}
	}

	// Build environment: inherit current env + add spawn env
	env := os.Environ()
	for _, e := range resp.ExecEnv {
		env = append(env, e)
	}

	// Replace this process with claude
	return syscall.Exec(claudePath, resp.ExecArgs, env)
}

// detectProject tries to determine the current project from git context.
// For worktrees, it finds the main repo name. For regular repos, uses the repo name.
func detectProject() string {
	// First, try to get the main worktree path (works for both worktrees and main repos)
	mainWorktree := getMainWorktreePath()
	if mainWorktree != "" {
		// Extract the repo name from the main worktree path
		return filepath.Base(mainWorktree)
	}

	// Fallback: try to get repo name from git remote
	cmd := exec.Command("git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err == nil {
		url := strings.TrimSpace(string(out))
		// Parse repo name from URL (handles both SSH and HTTPS)
		// git@github.com:user/repo.git -> repo
		// https://github.com/user/repo.git -> repo
		url = strings.TrimSuffix(url, ".git")
		parts := strings.Split(url, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// Last resort: use cwd basename
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Base(cwd)
}

// getMainWorktreePath returns the path to the main worktree (the original repo).
// For a regular repo, this returns the repo path itself.
// For a linked worktree, this returns the path to the main repo.
func getMainWorktreePath() string {
	// git worktree list --porcelain gives us all worktrees
	// The first one listed is always the main worktree
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			// First "worktree" line is the main worktree
			return strings.TrimPrefix(line, "worktree ")
		}
	}
	return ""
}

// runStatus shows a summary of active work.
func runStatus() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()

	// Get in-progress items
	inProgress, err := client.ListWorkItems(control.ListWorkItemsRequest{
		Project: project,
		Status:  "in_progress",
	})
	if err != nil {
		return err
	}

	// Get ready items
	ready, err := client.GetReadyItems(project)
	if err != nil {
		return err
	}

	printStatusBox(inProgress, ready)
	return nil
}

// Goal commands

func runGoalList() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	goals, err := client.ListWorkItems(control.ListWorkItemsRequest{
		Project:  project,
		ItemType: "goal",
	})
	if err != nil {
		return err
	}

	if len(goals) == 0 {
		fmt.Println(gray + "No goals found. Create one with: ath goal new \"Description\"" + reset)
		return nil
	}

	printWorkItemTable("Goals", goals)
	return nil
}

func runGoalNew(subject, description, project string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if project == "" {
		project = detectProject()
	}
	if project == "" {
		project = "default"
	}

	item, err := client.CreateWorkItem(control.CreateWorkItemRequest{
		Project:     project,
		ItemType:    "goal",
		Subject:     subject,
		Description: description,
	})
	if err != nil {
		return err
	}

	printSuccess(fmt.Sprintf("Created goal: %s", item.ID))
	printWorkItem(item, 0)

	// Save goal ID to context for auto-linking
	if err := cli.UpdateGoalContext(item.ID, item.Project); err != nil {
		// Non-fatal - just log the warning
		fmt.Fprintf(os.Stderr, "%sWarning: failed to save context: %v%s\n", yellow, err, reset)
	}

	return nil
}

func runGoalShow(id string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	tree, err := client.GetWorkItemTree(id, "")
	if err != nil {
		return err
	}

	if len(tree) == 0 {
		return fmt.Errorf("goal not found: %s\nHint: use 'ath goal' to list goals, or 'ath tree' to see all work items", id)
	}

	printWorkItemTree(tree)
	return nil
}

// Feature commands

func runFeatList() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	features, err := client.ListWorkItems(control.ListWorkItemsRequest{
		Project:  project,
		ItemType: "feature",
	})
	if err != nil {
		return err
	}

	if len(features) == 0 {
		fmt.Println(gray + "No features found. Create one with: ath feat new <goal-id> \"Description\"" + reset)
		return nil
	}

	printWorkItemTable("Features", features)
	return nil
}

func runFeatNew(parentID, subject, ticketID, description string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	// If no parent ID provided, try to use the last goal from context
	if parentID == "" {
		ctx, err := cli.LoadContext()
		if err == nil && ctx.LastGoalID != "" {
			parentID = ctx.LastGoalID
			fmt.Printf("%sUsing goal from context: %s%s\n", dim, parentID, reset)
		}
		// Otherwise parentID stays empty - standalone feature
	}

	project := detectProject()
	if project == "" {
		project = "default"
	}

	// If parent specified, inherit its project
	if parentID != "" {
		parent, err := client.GetWorkItem(parentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return fmt.Errorf("parent goal not found: %s\nHint: use 'ath goal' to list available goals", parentID)
		}
		project = parent.Project
	}

	item, err := client.CreateWorkItem(control.CreateWorkItemRequest{
		Project:     project,
		ItemType:    "feature",
		ParentID:    parentID,
		Subject:     subject,
		Description: description,
		TicketID:    ticketID,
	})
	if err != nil {
		return err
	}

	printSuccess(fmt.Sprintf("Created feature: %s", item.ID))
	printWorkItem(item, 0)

	// Save feature ID to context for auto-linking
	if err := cli.UpdateFeatureContext(item.ID, item.Project); err != nil {
		// Non-fatal - just log the warning
		fmt.Fprintf(os.Stderr, "%sWarning: failed to save context: %v%s\n", yellow, err, reset)
	}

	return nil
}

// Task commands

func runTskList(itemType string, allProjects bool) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	if allProjects {
		project = ""
	}

	// Map short types to full types
	title := "Tasks"
	switch itemType {
	case "goal":
		itemType = "goal"
		title = "Goals"
	case "feat":
		itemType = "feature"
		title = "Features"
	case "":
		itemType = "task"
	}

	items, err := client.ListWorkItems(control.ListWorkItemsRequest{
		Project:  project,
		ItemType: itemType,
	})
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println(gray + "No items found" + reset)
		return nil
	}

	printWorkItemTable(title, items)
	return nil
}

func runTskCreate(featureID string, subjects []string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	if project == "" {
		project = "default"
	}

	for _, subject := range subjects {
		item, err := client.CreateWorkItem(control.CreateWorkItemRequest{
			Project:  project,
			ItemType: "task",
			ParentID: featureID, // Empty = orphan/inbox
			Subject:  subject,
		})
		if err != nil {
			printError(fmt.Sprintf("Failed to create task: %v", err))
			continue
		}

		printSuccess(fmt.Sprintf("Created: %s", item.ID))
		printWorkItem(item, 0)
	}
	return nil
}

func runTskReady() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	ready, err := client.GetReadyItems(project)
	if err != nil {
		return err
	}

	if len(ready) == 0 {
		fmt.Println(gray + "No ready items" + reset)
		return nil
	}

	fmt.Println(bold + "Ready to work:" + reset)
	for _, item := range ready {
		printWorkItem(item, 0)
	}
	return nil
}

func runTskInteractive() error {
	// For now, just show status and prompt
	fmt.Println(bold + "Interactive mode" + reset)
	fmt.Println(gray + "(Full interactive mode coming soon)" + reset)
	fmt.Println()
	return runStatus()
}

// Tree command

func runTree(rootID, project string, goalsOnly bool) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if project == "" {
		project = detectProject()
	}

	var items []*control.WorkItemInfo

	if rootID != "" {
		// Get tree from specific root
		items, err = client.GetWorkItemTree(rootID, "")
	} else if goalsOnly {
		// Just goals
		items, err = client.ListWorkItems(control.ListWorkItemsRequest{
			Project:  project,
			ItemType: "goal",
		})
	} else {
		// Full tree - get goals and expand
		items, err = client.GetWorkItemTree("", project)
	}

	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println(gray + "No work items found" + reset)
		fmt.Println("Create a goal with: ath goal new \"Description\"")
		return nil
	}

	printWorkItemTree(items)
	return nil
}

// Worktree command

func runWtList() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	worktrees, err := client.ListWorktrees()
	if err != nil {
		return err
	}

	// Filter by git worktree membership if we're in a project context
	project := detectProject()
	projectWorktrees := getProjectWorktreePaths()
	if len(projectWorktrees) > 0 {
		filtered := make([]*control.WorktreeInfo, 0)
		for _, wt := range worktrees {
			if _, ok := projectWorktrees[wt.Path]; ok {
				filtered = append(filtered, wt)
			}
		}
		worktrees = filtered
	}

	// Get queue info to show position
	var queuePositions map[string]int
	if project != "" {
		queueItems, err := client.GetMergeQueue(project)
		if err == nil {
			queuePositions = make(map[string]int)
			for _, item := range queueItems {
				queuePositions[item.WorktreePath] = item.Position
			}
		}
	}

	// Get ahead/behind info for each worktree
	aheadBehind := getWorktreeAheadBehind(worktrees)

	printWorktreeTableWithQueue(worktrees, queuePositions, aheadBehind)
	return nil
}

// AheadBehind holds git ahead/behind counts
type AheadBehind struct {
	Ahead  int
	Behind int
}

// getWorktreeAheadBehind fetches ahead/behind counts for worktrees
func getWorktreeAheadBehind(worktrees []*control.WorktreeInfo) map[string]AheadBehind {
	result := make(map[string]AheadBehind)
	for _, wt := range worktrees {
		if wt.IsMain {
			continue
		}
		cmd := exec.Command("git", "-C", wt.Path, "rev-list", "--left-right", "--count", "main...HEAD")
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) == 2 {
			behind, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			ahead, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			result[wt.Path] = AheadBehind{Ahead: ahead, Behind: behind}
		}
	}
	return result
}

// getProjectWorktreePaths returns all worktree paths belonging to the current git repo.
func getProjectWorktreePaths() map[string]bool {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	paths := make(map[string]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			paths[path] = true
		}
	}
	return paths
}

// runWtPrune cleans up merged and orphaned worktrees
func runWtPrune() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	result, err := client.PruneWorktrees()
	if err != nil {
		return err
	}

	total := 0

	// Show pruned merged worktrees
	if len(result.Merged) > 0 {
		for _, path := range result.Merged {
			fmt.Printf("%sPruned (merged):%s %s\n", green, reset, path)
			total++
		}
	}

	// Show pruned orphaned directories
	if len(result.Orphans) > 0 {
		for _, path := range result.Orphans {
			fmt.Printf("%sPruned (orphan):%s %s\n", yellow, reset, path)
			total++
		}
	}

	// Show pruned stale entries
	if len(result.Stale) > 0 {
		for _, path := range result.Stale {
			fmt.Printf("%sPruned (stale):%s %s\n", gray, reset, path)
			total++
		}
	}

	if total == 0 {
		fmt.Println(dim + "No worktrees to prune." + reset)
	} else {
		fmt.Printf("\n%s%d worktree(s) pruned.%s\n", bold, total, reset)
	}

	return nil
}

// ============================================================================
// Merge Queue Commands
// ============================================================================

func runQueueList(project string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	// Auto-detect project if not provided
	if project == "" {
		project = detectProject()
	}

	items, err := client.GetMergeQueue(project)
	if err != nil {
		return err
	}

	// Check if a VCS plugin is enabled for full functionality.
	cfg, err := plugin.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sWarning:%s failed to load plugin config: %v\n", yellow, reset, err)
	}
	vcsEnabled := cfg != nil && (cfg.IsEnabled("github") || cfg.IsEnabled("gitlab"))

	if len(items) == 0 {
		fmt.Println(gray + "Queue empty - new worktrees will branch from main" + reset)
		if !vcsEnabled {
			fmt.Println()
			fmt.Println(gray + "Tip: Enable a VCS plugin for automatic PR sync:" + reset)
			fmt.Println(gray + "  ath plugin enable github" + reset)
		}
		return nil
	}

	fmt.Printf("%s≡%s Merge Queue (%d items)\n\n", cyan, reset, len(items))

	for _, item := range items {
		statusIcon := getQueueStatusIcon(item.Status)
		branchName := filepath.Base(item.Branch)

		// Truncate path for display
		displayPath := item.WorktreePath
		if len(displayPath) > 40 {
			displayPath = "..." + displayPath[len(displayPath)-37:]
		}

		fmt.Printf("  %s %s#%d%s %s\n",
			statusIcon,
			yellow, item.Position, reset,
			branchName,
		)
		fmt.Printf("      %spath:%s %s\n", gray, reset, displayPath)

		if item.Status == "rebasing" || item.Status == "diverged" {
			fmt.Printf("      %sneeds reconcile%s\n", yellow, reset)
		}
	}

	return nil
}

func runQueueAdd(path string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	item, err := client.AddToMergeQueue(control.AddToMergeQueueRequest{
		WorktreePath: absPath,
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s%s%s Added to queue at position #%d\n", green, checkMark, reset, item.Position)
	fmt.Printf("   Branch: %s\n", item.Branch)
	fmt.Printf("   Based on: %s (%s)\n", item.BaseBranch, shortSHA(item.BaseCommit))

	return nil
}

func runQueueHead(project string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if project == "" {
		project = detectProject()
	}

	head, err := client.GetMergeQueueHead(project)
	if err != nil {
		return err
	}

	if head.Empty {
		fmt.Println(gray + "Queue empty - base new worktrees on main" + reset)
		return nil
	}

	fmt.Printf("%s≡%s Integration HEAD\n", cyan, reset)
	fmt.Printf("   Branch: %s\n", head.Branch)
	if head.Commit != "" {
		fmt.Printf("   Commit: %s\n", shortSHA(head.Commit))
	}
	fmt.Println()
	fmt.Println(gray + "New worktrees should branch from this point" + reset)

	return nil
}

func runQueueBump(path string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	item, err := client.BumpMergeQueueItem(absPath)
	if err != nil {
		return err
	}

	fmt.Printf("%s%s%s Updated queue head at position #%d\n", green, checkMark, reset, item.Position)
	fmt.Println(yellow + "Dependent features were reconciled or marked diverged" + reset)

	return nil
}

func runQueueRemove(path string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := client.RemoveFromMergeQueue(absPath); err != nil {
		return err
	}

	fmt.Printf("%s%s%s Removed from queue\n", green, checkMark, reset)
	return nil
}

func runQueueGraph(project string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if project == "" {
		project = detectProject()
	}

	items, err := client.GetMergeQueue(project)
	if err != nil {
		return err
	}

	// Determine base branch from first item or default to "main"
	baseBranch := "main"
	if len(items) > 0 && items[0].BaseBranch != "" {
		baseBranch = items[0].BaseBranch
	}

	printQueueGraph(items, baseBranch)
	return nil
}

func runQueueReconcile(project string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if project == "" {
		project = detectProject()
	}

	result, err := client.ReconcileQueue(project)
	if err != nil {
		return err
	}

	printReconcileResults(result.Results)
	return nil
}

// Helper functions for queue display

func getQueueStatusIcon(status string) string {
	switch status {
	case "queued":
		return cyan + "[Q]" + reset
	case "merging":
		return green + "[M]" + reset
	case "rebasing":
		return yellow + "[R]" + reset
	case "diverged":
		return yellow + "[D]" + reset
	case "conflict":
		return red + "[!]" + reset
	default:
		return gray + "[?]" + reset
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// ============================================================================
// Agent Commands
// ============================================================================

func runAgentList(statusFilter string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	agents, err := client.ListAgents()
	if err != nil {
		return err
	}

	// Apply status filter
	if statusFilter != "" {
		var filtered []*control.AgentInfo
		for _, a := range agents {
			if a.Status == statusFilter {
				filtered = append(filtered, a)
			}
		}
		agents = filtered
	}

	printAgentTable(agents)
	return nil
}

func runAgentShow(id string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	// Support prefix matching - list all and find match
	agents, err := client.ListAgents()
	if err != nil {
		return err
	}

	var match *control.AgentInfo
	for _, a := range agents {
		if a.ID == id || strings.HasPrefix(a.ID, id) {
			if match != nil {
				return fmt.Errorf("ambiguous agent ID prefix %q: matches both %s and %s\nHint: use a longer prefix to disambiguate", id, match.ID[:8], a.ID[:8])
			}
			match = a
		}
	}
	if match == nil {
		return fmt.Errorf("agent not found: %s\nHint: use 'ath agent' to list all agents", id)
	}

	printAgentDetail(match)
	return nil
}

// ============================================================================
// Tidy Command
// ============================================================================

func runTidy(headless bool) error {
	return runSpawn("", "", "reconciler", false, headless, false, false)
}

// ============================================================================
// Map Command
// ============================================================================

func runMap(headless bool) error {
	return runSpawn("", "", "mapper", false, headless, false, false)
}

// ============================================================================
// Auto-Run Commands
// ============================================================================

func runAutoRun(project string, once bool) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if project == "" {
		project = detectProject()
	}

	status, err := client.StartAutoRun(control.AutoRunRequest{
		Project: project,
		Once:    once,
	})
	if err != nil {
		return err
	}

	if once {
		fmt.Printf("%s%s%s Auto-run started (one task)\n", green, checkMark, reset)
		fmt.Printf("  Project: %s\n", status.Project)
		fmt.Println()

		// Wait for completion and show results
		for {
			time.Sleep(3 * time.Second)
			s, err := client.GetAutoRunStatus()
			if err != nil {
				return fmt.Errorf("lost connection while waiting: %w", err)
			}
			if s.Running {
				if s.CurrentItem != nil {
					fmt.Printf("\r  %sWorking on: %s %s%s", dim, s.CurrentItem.ID, s.CurrentItem.Subject, reset)
				}
				continue
			}
			fmt.Println()
			printAutoRunResults(s)
			return nil
		}
	}

	fmt.Printf("%s%s%s Auto-run loop started\n", green, checkMark, reset)
	fmt.Printf("  Project: %s\n", status.Project)
	fmt.Println()
	fmt.Printf("%sMonitor: ath run status%s\n", dim, reset)
	fmt.Printf("%sStop:    ath run stop%s\n", dim, reset)
	return nil
}

func runAutoRunStatus() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	status, err := client.GetAutoRunStatus()
	if err != nil {
		return err
	}

	if !status.Running {
		fmt.Println(dim + "Auto-run is not active" + reset)
		if status.Completed > 0 || status.Failed > 0 {
			printAutoRunResults(status)
		}
		return nil
	}

	fmt.Printf("%s%s%s Auto-run active\n", green, checkMark, reset)
	fmt.Printf("  Project:   %s\n", status.Project)

	if status.CurrentItem != nil {
		fmt.Printf("  Working on: %s%s%s %s\n", magenta, status.CurrentItem.ID, reset, status.CurrentItem.Subject)
	}
	if status.CurrentAgent != nil {
		agentID := status.CurrentAgent.ID
		if len(agentID) > 8 {
			agentID = agentID[:8]
		}
		fmt.Printf("  Agent:     %s%s%s (%s)\n", yellow, agentID, reset, status.CurrentAgent.Status)
	}
	fmt.Println()
	printAutoRunResults(status)

	return nil
}

func printAutoRunResults(status *control.AutoRunStatus) {
	if status.Completed > 0 {
		fmt.Printf("  %s%d completed%s\n", green, status.Completed, reset)
		for _, r := range status.CompletedItems {
			agentHint := ""
			if r.AgentID != "" {
				aid := r.AgentID
				if len(aid) > 8 {
					aid = aid[:8]
				}
				agentHint = fmt.Sprintf(" %s(agent %s)%s", dim, aid, reset)
			}
			fmt.Printf("    %s%s%s %s %s%s\n", green, checkMark, reset, r.ItemID, r.Subject, agentHint)
		}
	}
	if status.Failed > 0 {
		fmt.Printf("  %s%d failed%s\n", red, status.Failed, reset)
		for _, r := range status.FailedItems {
			agentHint := ""
			if r.AgentID != "" {
				aid := r.AgentID
				if len(aid) > 8 {
					aid = aid[:8]
				}
				agentHint = fmt.Sprintf(" %s(agent %s)%s", dim, aid, reset)
			}
			fmt.Printf("    %s✗%s %s %s%s\n", red, reset, r.ItemID, r.Subject, agentHint)
			if r.Error != "" {
				fmt.Printf("      %s%s%s\n", dim, r.Error, reset)
			}
			if r.AgentID != "" {
				aid := r.AgentID
				if len(aid) > 8 {
					aid = aid[:8]
				}
				fmt.Printf("      %sLogs: ath agent %s%s\n", dim, aid, reset)
			}
		}
	}
}

func runAutoRunStop() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	status, err := client.StopAutoRun()
	if err != nil {
		return err
	}

	fmt.Printf("%s%s%s Auto-run stopped\n", green, checkMark, reset)
	printAutoRunResults(status)
	return nil
}

// ============================================================================
// Rate Limit Command
// ============================================================================

func runRateStatus() error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	status, err := client.GetRateLimitStatus()
	if err != nil {
		return err
	}

	if status.Limited {
		fmt.Printf("%s!%s Rate limited\n", yellow, reset)
		if status.WaitingSec > 0 {
			fmt.Printf("  Reset:   in %ds (%s)\n", status.WaitingSec, status.ResetAt)
		} else {
			fmt.Printf("  Reset:   %s\n", status.ResetAt)
		}
		if status.Reason != "" {
			reason := status.Reason
			if len(reason) > 80 {
				reason = reason[:77] + "..."
			}
			fmt.Printf("  Reason:  %s\n", reason)
		}
		if status.AgentID != "" {
			agentID := status.AgentID
			if len(agentID) > 8 {
				agentID = agentID[:8]
			}
			fmt.Printf("  Trigger: agent %s%s%s\n", magenta, agentID, reset)
		}
	} else {
		fmt.Printf("%s%s%s No rate limit active\n", green, checkMark, reset)
	}

	if status.HitCount > 0 {
		fmt.Printf("  Total hits: %d this session\n", status.HitCount)
	}

	return nil
}

// ============================================================================
// Session Commands
// ============================================================================

func runSessionShow(agentID string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	// Support prefix matching - list all and find match
	agents, err := client.ListAgents()
	if err != nil {
		return err
	}

	var match *control.AgentInfo
	for _, a := range agents {
		if a.ID == agentID || strings.HasPrefix(a.ID, agentID) {
			if match != nil {
				return fmt.Errorf("ambiguous agent ID prefix %q: matches both %s and %s\nHint: use a longer prefix to disambiguate", agentID, match.ID[:8], a.ID[:8])
			}
			match = a
		}
	}
	if match == nil {
		return fmt.Errorf("agent not found: %s\nHint: use 'ath agent' to list all agents", agentID)
	}

	if match.ClaudeSessionID == "" {
		return fmt.Errorf("agent %s has no session ID", match.ID[:8])
	}

	// Print the session ID and jump command
	fmt.Printf("%s%s%s\n", yellow, match.ClaudeSessionID, reset)
	fmt.Printf("\n%sJump into this session:%s\n", dim, reset)
	fmt.Printf("  claude --session-id %s\n", match.ClaudeSessionID)

	// Show worktree context if available
	if match.WorktreePath != "" {
		fmt.Printf("\n%sWorktree:%s %s%s%s\n", dim, reset, cyan, match.WorktreePath, reset)
		fmt.Printf("%sChange directory first if needed:%s\n", dim, reset)
		fmt.Printf("  cd %s\n", match.WorktreePath)
	}

	return nil
}

// ============================================================================
// Plugin Commands
// ============================================================================

// Available plugins (static list for now - could be dynamic later)
var availablePlugins = []struct {
	name     string
	category string
	desc     string
}{
	{"github", "vcs", "GitHub - PRs, CI/CD via gh CLI"},
	{"gitlab", "vcs", "GitLab - MRs, CI/CD via glab CLI"},
	{"linear", "pm", "Linear - Issue tracking"},
	{"jira", "pm", "Jira - Issue tracking via REST API"},
}

func isKnownPlugin(name string) bool {
	for _, p := range availablePlugins {
		if p.name == name {
			return true
		}
	}
	return false
}

func runPluginList(category string) error {
	cfg, err := plugin.LoadConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%sPlugins%s\n\n", bold, reset)

	currentCat := ""
	for _, p := range availablePlugins {
		if category != "" && p.category != category {
			continue
		}

		// Print category header
		if p.category != currentCat {
			currentCat = p.category
			catName := "Version Control"
			if currentCat == "pm" {
				catName = "Project Management"
			}
			fmt.Printf("%s%s%s\n", cyan, catName, reset)
		}

		// Status indicator
		enabled := cfg.IsEnabled(p.name)
		status := gray + "[ ]" + reset
		if enabled {
			status = green + "[*]" + reset
		}

		fmt.Printf("  %s %s%s%s - %s\n", status, bold, p.name, reset, p.desc)
	}

	fmt.Println()
	fmt.Println(gray + "Use 'ath plugin enable <name>' to enable a plugin" + reset)
	return nil
}

func runPluginEnable(name string) error {
	return runPluginSetEnabled(name, true)
}

func runPluginDisable(name string) error {
	return runPluginSetEnabled(name, false)
}

func runPluginSetEnabled(name string, enabled bool) error {
	if !isKnownPlugin(name) {
		knownNames := make([]string, 0, len(availablePlugins))
		for _, p := range availablePlugins {
			knownNames = append(knownNames, p.name)
		}
		return fmt.Errorf("unknown plugin %q: available plugins are: %s", name, strings.Join(knownNames, ", "))
	}

	cfg, err := plugin.LoadConfig()
	if err != nil {
		return err
	}

	cfg.Enabled[name] = enabled
	if err := plugin.SaveConfig(cfg); err != nil {
		return err
	}

	verb := "Disabled"
	if enabled {
		verb = "Enabled"
	}
	fmt.Printf("%s%s%s %s %s\n", green, checkMark, reset, verb, name)
	return nil
}
