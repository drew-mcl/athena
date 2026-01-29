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
