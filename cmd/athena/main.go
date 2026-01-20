// Command athena is the Athena CLI and TUI.
package main

import (
	"fmt"
	"os"

	"github.com/drewfead/athena/internal/config"
	"github.com/spf13/cobra"
)

var cfg *config.Config

func main() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "athena",
	Short: "Orchestration platform for Claude Code agents",
	Long: `Athena is a Kubernetes-like control plane for Claude Code agents.

It manages agent lifecycles, worktrees, and provides a TUI dashboard
for monitoring and interacting with your AI-assisted development workflow.`,
	RunE: runDashboard,
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Daemon management commands",
	Hidden: true,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemonStatus()
	},
}

var viewCmd = &cobra.Command{
	Use:   "view <agent-id>",
	Short: "View live output from an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentView(args[0])
	},
}

// Migration commands
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate worktrees to the standard directory structure",
	Long: `Migrate existing worktrees to the centralized ~/repos/worktrees/ directory.

By default, shows a plan of what would be migrated.
Use --execute to perform the actual migration.

Examples:
  athena migrate           # Show migration plan
  athena migrate --execute # Perform the migration`,
	RunE: func(cmd *cobra.Command, args []string) error {
		execute, _ := cmd.Flags().GetBool("execute")
		return runMigrate(execute)
	},
}

// Changelog commands
var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Manage changelog entries",
	Long:  "Track features, fixes, and changes made to Athena and its managed projects.",
}

var changelogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List changelog entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		limit, _ := cmd.Flags().GetInt("limit")
		return runChangelogList(project, limit)
	},
}

var changelogAddCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Add a changelog entry",
	Long: `Add a new changelog entry to track features, fixes, or changes.

Categories:
  feature  - New functionality (✨)
  fix      - Bug fixes (🔧)
  refactor - Code improvements (♻️)
  docs     - Documentation (📚)

Examples:
  athena changelog add "Add structured logging" -c feature -d "Integrated slog with Sentry"
  athena changelog add "Fix signal handling" -c fix -p athena`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := args[0]
		description, _ := cmd.Flags().GetString("description")
		category, _ := cmd.Flags().GetString("category")
		project, _ := cmd.Flags().GetString("project")
		return runChangelogAdd(title, description, category, project)
	},
}

func init() {
	// TUI debug flags
	rootCmd.PersistentFlags().BoolVar(&debugKeys, "debug-keys", false, "Log keystrokes for TUI debugging")
	rootCmd.PersistentFlags().StringVar(&debugLogPath, "debug-log", "", "Write TUI debug logs to this file")

	// Migration flags
	migrateCmd.Flags().BoolP("execute", "e", false, "Execute the migration (default: show plan only)")

	// Changelog flags
	changelogListCmd.Flags().StringP("project", "p", "", "Filter by project")
	changelogListCmd.Flags().IntP("limit", "l", 20, "Maximum entries to show")

	changelogAddCmd.Flags().StringP("description", "d", "", "Detailed description")
	changelogAddCmd.Flags().StringP("category", "c", "feature", "Category: feature|fix|refactor|docs")
	changelogAddCmd.Flags().StringP("project", "p", "athena", "Project name")

	changelogCmd.AddCommand(changelogListCmd, changelogAddCmd)

	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd, viewCmd, changelogCmd, migrateCmd)
}
