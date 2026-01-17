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

	model := dashboard.New(client)
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
