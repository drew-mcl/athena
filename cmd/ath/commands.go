package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
)

// Scratchpad entry
type ScratchpadEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func getScratchpadPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "athena", "scratchpad.json")
}

func loadScratchpad() ([]ScratchpadEntry, error) {
	path := getScratchpadPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []ScratchpadEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func saveScratchpad(entries []ScratchpadEntry) error {
	path := getScratchpadPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func runSpList() error {
	entries, err := loadScratchpad()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println(gray + "Scratchpad empty. Add ideas with: ath sp \"your idea\"" + reset)
		return nil
	}

	fmt.Println(bold + "Scratchpad" + reset)
	fmt.Println(gray + strings.Repeat("─", 60) + reset)

	for i, entry := range entries {
		// Format timestamp
		ts := entry.CreatedAt.Format("Jan 2 15:04")

		// Print entry number and timestamp
		fmt.Printf("%s#%d%s  %s%s%s\n", yellow, i+1, reset, gray, ts, reset)

		// Print content with nice formatting
		lines := strings.Split(entry.Content, "\n")
		for _, line := range lines {
			if line != "" {
				fmt.Printf("  %s\n", line)
			} else {
				fmt.Println()
			}
		}
		fmt.Println()
	}

	return nil
}

func runSpAdd(text string) error {
	entries, err := loadScratchpad()
	if err != nil {
		return err
	}

	// Check if input is from pipe (multi-line)
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Reading from pipe
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if len(lines) > 0 {
			text = strings.Join(lines, "\n")
		}
	}

	entry := ScratchpadEntry{
		ID:        fmt.Sprintf("sp-%d", time.Now().UnixNano()),
		Content:   text,
		CreatedAt: time.Now(),
	}

	entries = append(entries, entry)

	if err := saveScratchpad(entries); err != nil {
		return err
	}

	fmt.Printf("%sAdded to scratchpad%s (#%d)\n", green, reset, len(entries))
	return nil
}

// detectProject tries to determine the current project from cwd.
func detectProject() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	// Extract project name from path (last component of repos path)
	parts := strings.Split(cwd, "/")
	for i, p := range parts {
		if p == "repos" && i+1 < len(parts) {
			return parts[i+1]
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

func runTskList(itemType string) error {
	client, err := getClient()
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer client.Close()

	project := detectProject()

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

	printWorktreeTable(worktrees)
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

		if item.Status == "rebasing" {
			fmt.Printf("      %sneeds rebase%s\n", yellow, reset)
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

	fmt.Printf("%s%s%s Moved to position #%d\n", green, checkMark, reset, item.Position)
	fmt.Println(yellow + "Dependent features marked for rebase" + reset)

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
	{"jira", "pm", "Jira - Issue tracking (coming soon)"},
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
