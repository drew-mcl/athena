package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/tui/dashboard"
	"github.com/drewfead/athena/internal/tui/viewer"
)

func runDashboard(cmd *cobra.Command, args []string) error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running? Start it with: athenad", err)
	}
	defer client.Close()

	model := dashboard.New(client, cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func runDaemonStatus() error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		fmt.Println("Daemon status: NOT RUNNING")
		fmt.Printf("Socket: %s\n", cfg.Daemon.Socket)
		return nil
	}
	defer client.Close()

	fmt.Println("Daemon status: RUNNING")
	fmt.Printf("Socket: %s\n", cfg.Daemon.Socket)

	agents, _ := client.ListAgents()
	worktrees, _ := client.ListWorktrees()
	jobs, _ := client.ListJobs()

	fmt.Printf("Agents: %d\n", len(agents))
	fmt.Printf("Worktrees: %d\n", len(worktrees))
	fmt.Printf("Jobs: %d\n", len(jobs))

	return nil
}

func runAgentView(agentID string) error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running?", err)
	}
	defer client.Close()

	model := viewer.New(client, agentID)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// Changelog commands

func runChangelogList(project string, limit int) error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running?", err)
	}
	defer client.Close()

	entries, err := client.ListChangelog(project, limit)
	if err != nil {
		return fmt.Errorf("failed to list changelog: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No changelog entries found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tTITLE\tPROJECT\tDATE")
	fmt.Fprintln(w, "--------\t-----\t-------\t----")

	for _, e := range entries {
		icon := categoryIcon(e.Category)
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\n", icon, e.Category, e.Title, e.Project, e.CreatedAt[:10])
	}
	w.Flush()

	return nil
}

func runChangelogAdd(title, description, category, project string) error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running?", err)
	}
	defer client.Close()

	// Validate category
	category = strings.ToLower(category)
	validCategories := map[string]bool{"feature": true, "fix": true, "refactor": true, "docs": true}
	if !validCategories[category] {
		return fmt.Errorf("invalid category %q: must be one of feature, fix, refactor, docs", category)
	}

	entry, err := client.CreateChangelog(control.CreateChangelogRequest{
		Title:       title,
		Description: description,
		Category:    category,
		Project:     project,
	})
	if err != nil {
		return fmt.Errorf("failed to create changelog entry: %w", err)
	}

	fmt.Printf("%s Added: %s\n", categoryIcon(entry.Category), entry.Title)
	return nil
}

func categoryIcon(category string) string {
	switch category {
	case "feature":
		return "✨"
	case "fix":
		return "🔧"
	case "refactor":
		return "♻️"
	case "docs":
		return "📚"
	default:
		return "•"
	}
}

// Migration commands

func runMigratePlan() error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running?", err)
	}
	defer client.Close()

	plan, err := client.MigratePlan()
	if err != nil {
		return fmt.Errorf("failed to get migration plan: %w", err)
	}

	if len(plan.Migrations) == 0 {
		fmt.Println("No worktrees need migration.")
		fmt.Printf("Worktree directory: %s\n", plan.WorktreeDir)
		return nil
	}

	fmt.Printf("Migration Plan\n")
	fmt.Printf("==============\n")
	fmt.Printf("Target directory: %s\n\n", plan.WorktreeDir)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TICKET\tCURRENT\tTARGET")
	fmt.Fprintln(w, "------\t-------\t------")

	for _, m := range plan.Migrations {
		current := shortenPath(m.CurrentPath)
		target := shortenPath(m.TargetPath)
		fmt.Fprintf(w, "%s\t%s\t%s\n", m.TicketID, current, target)
	}
	w.Flush()

	fmt.Printf("\n%d worktree(s) will be migrated.\n", len(plan.Migrations))
	fmt.Println("Run 'athena migrate --execute' to perform the migration.")

	return nil
}

func runMigrate(execute bool) error {
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running?", err)
	}
	defer client.Close()

	if !execute {
		return runMigratePlan()
	}

	migrated, err := client.MigrateWorktrees(false)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	if len(migrated) == 0 {
		fmt.Println("No worktrees needed migration.")
		return nil
	}

	fmt.Printf("Successfully migrated %d worktree(s):\n", len(migrated))
	for _, path := range migrated {
		fmt.Printf("  %s\n", shortenPath(path))
	}

	return nil
}

func shortenPath(path string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
