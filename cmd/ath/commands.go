package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/drewfead/athena/internal/control"
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

func runSpawn(featureID, id string, retrieve, headless, worktree, parallel bool) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()
	cwd, _ := os.Getwd()

	req := control.SpawnRequest{
		Project:  project,
		Retrieve: retrieve,
		Headless: headless,
		Worktree: worktree,
		Parallel: parallel,
		WorkDir:  cwd,
	}

	// Feature flag takes priority
	if featureID != "" {
		req.FeatureID = featureID
	} else if id != "" {
		// Classify the positional ID argument
		if isWorkItemID(id) {
			req.WorkItemID = id
		} else {
			// Treat as ticket ID
			req.TicketID = strings.ToUpper(id)
		}
	}

	resp, err := client.Spawn(req)
	if err != nil {
		return err
	}

	// Print context summary
	if headless && resp.Agent != nil {
		fmt.Printf("%s%s%s Spawned agent %s%s%s\n", green, checkMark, reset, magenta, resp.Agent.ID[:8], reset)
	} else {
		fmt.Printf("%s%s%s Launching Claude Code\n", green, checkMark, reset)
	}
	fmt.Printf("  Work item: %s%s%s  %s\n", magenta, resp.WorkItem.ID, reset, resp.WorkItem.Subject)
	fmt.Printf("  Task list: %s%s%s\n", gray, resp.TaskListID, reset)
	if resp.Worktree != nil {
		fmt.Printf("  Worktree:  %s%s%s\n", cyan, resp.Worktree.Path, reset)
	}
	fmt.Println()

	if headless {
		fmt.Println(dim + "Check progress: ath tree " + resp.WorkItem.ID + reset)
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
		return fmt.Errorf("goal not found: %s", id)
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

	// Get parent to inherit project
	parent, err := client.GetWorkItem(parentID)
	if err != nil {
		return err
	}
	if parent == nil {
		return fmt.Errorf("parent goal not found: %s", parentID)
	}

	item, err := client.CreateWorkItem(control.CreateWorkItemRequest{
		Project:     parent.Project,
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
			behind, _ := strconv.Atoi(parts[0])
			ahead, _ := strconv.Atoi(parts[1])
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

	// Check if VCS plugin is enabled for full functionality
	cfg, _ := loadPluginConfig()
	vcsEnabled := cfg != nil && (cfg.Enabled["github"] || cfg.Enabled["gitlab"])

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

// Plugin state stored in config file
func getPluginConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "athena", "plugins.json")
}

type PluginConfig struct {
	Enabled map[string]bool `json:"enabled"`
}

func loadPluginConfig() (*PluginConfig, error) {
	path := getPluginConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PluginConfig{Enabled: make(map[string]bool)}, nil
		}
		return nil, err
	}

	var cfg PluginConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Enabled == nil {
		cfg.Enabled = make(map[string]bool)
	}
	return &cfg, nil
}

func savePluginConfig(cfg *PluginConfig) error {
	path := getPluginConfigPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func runPluginList(category string) error {
	cfg, err := loadPluginConfig()
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
		enabled := cfg.Enabled[p.name]
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
	// Validate plugin exists
	found := false
	for _, p := range availablePlugins {
		if p.name == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown plugin: %s", name)
	}

	cfg, err := loadPluginConfig()
	if err != nil {
		return err
	}

	cfg.Enabled[name] = true
	if err := savePluginConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("%s%s%s Enabled %s\n", green, checkMark, reset, name)
	return nil
}

func runPluginDisable(name string) error {
	cfg, err := loadPluginConfig()
	if err != nil {
		return err
	}

	cfg.Enabled[name] = false
	if err := savePluginConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("%s%s%s Disabled %s\n", green, checkMark, reset, name)
	return nil
}
