// Command athena is the Athena CLI and TUI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/index"
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

// Index commands
var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage the code index for context optimization",
	Long:  "Build and query the symbol index and dependency graph for smarter context filtering.",
}

var indexBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the symbol index and dependency graph",
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		return runIndexBuild(project)
	},
}

var indexQueryCmd = &cobra.Command{
	Use:   "query <symbol>",
	Short: "Look up where a symbol is defined",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		return runIndexQuery(args[0], project)
	},
}

var indexDepsCmd = &cobra.Command{
	Use:   "deps <file>",
	Short: "Show dependencies for a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		reverse, _ := cmd.Flags().GetBool("reverse")
		return runIndexDeps(args[0], project, reverse)
	},
}

var indexRelevantCmd = &cobra.Command{
	Use:   "relevant <task>",
	Short: "Find files relevant to a task description",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		limit, _ := cmd.Flags().GetInt("limit")
		return runIndexRelevant(args[0], project, limit)
	},
}

func runIndexBuild(projectPath string) error {
	path, err := resolveProjectPath(projectPath)
	if err != nil {
		return err
	}

	fmt.Printf("Building index for %s...\n", path)
	start := time.Now()

	indexer, err := index.NewIndexer(path)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	idx, err := indexer.IndexProject()
	if err != nil {
		return fmt.Errorf("failed to build index: %w", err)
	}

	stats := idx.Stats()
	fmt.Printf("Index built in %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("  Files indexed: %d\n", stats.TotalFiles)
	fmt.Printf("  Symbols found: %d\n", stats.TotalSymbols)
	fmt.Printf("  Dependencies:  %d\n", stats.TotalDeps)
	return nil
}

func runIndexQuery(symbol, projectPath string) error {
	path, err := resolveProjectPath(projectPath)
	if err != nil {
		return err
	}

	indexer, err := index.NewIndexer(path)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	idx, err := indexer.IndexProject()
	if err != nil {
		return fmt.Errorf("failed to build index: %w", err)
	}

	results := idx.LookupSymbol(symbol)
	if len(results) == 0 {
		fmt.Printf("No definitions found for '%s'\n", symbol)
		return nil
	}

	fmt.Printf("%s (%s) defined in:\n", symbol, results[0].Kind)
	for _, sym := range results {
		fmt.Printf("  %s:%d\n", sym.FilePath, sym.LineNumber)
	}
	return nil
}

func runIndexDeps(filePath, projectPath string, reverse bool) error {
	path, err := resolveProjectPath(projectPath)
	if err != nil {
		return err
	}

	indexer, err := index.NewIndexer(path)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	idx, err := indexer.IndexProject()
	if err != nil {
		return fmt.Errorf("failed to build index: %w", err)
	}

	if reverse {
		deps := idx.GetDependents(filePath)
		if len(deps) == 0 {
			fmt.Printf("No files import %s\n", filePath)
			return nil
		}
		fmt.Printf("%s is imported by:\n", filePath)
		for _, dep := range deps {
			fmt.Printf("  %s\n", dep)
		}
	} else {
		deps := idx.GetDependencies(filePath)
		if len(deps) == 0 {
			fmt.Printf("%s has no imports\n", filePath)
			return nil
		}
		fmt.Printf("%s imports:\n", filePath)
		for _, dep := range deps {
			if dep.IsInternal {
				fmt.Printf("  %s\n", dep.ToFile)
			} else {
				fmt.Printf("  %s (external)\n", dep.ImportPath)
			}
		}
	}
	return nil
}

func runIndexRelevant(task, projectPath string, limit int) error {
	path, err := resolveProjectPath(projectPath)
	if err != nil {
		return err
	}

	indexer, err := index.NewIndexer(path)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	idx, err := indexer.IndexProject()
	if err != nil {
		return fmt.Errorf("failed to build index: %w", err)
	}

	scorer := index.NewScorer(idx)
	results := scorer.ScoreFiles(task, path, limit)
	if len(results) == 0 {
		fmt.Printf("No relevant files found for: %s\n", task)
		return nil
	}

	fmt.Printf("Relevant files for \"%s\":\n", task)
	for i, r := range results {
		fmt.Printf("%2d. %s (%.2f) - %s\n", i+1, r.Path, r.Score, r.Reason)
	}
	return nil
}

func resolveProjectPath(path string) (string, error) {
	if path == "" {
		return os.Getwd()
	}
	return filepath.Abs(path)
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

	// Index flags
	indexBuildCmd.Flags().StringP("project", "p", "", "Project path (default: current directory)")
	indexQueryCmd.Flags().StringP("project", "p", "", "Project path")
	indexDepsCmd.Flags().StringP("project", "p", "", "Project path")
	indexDepsCmd.Flags().BoolP("reverse", "r", false, "Show files that import this file")
	indexRelevantCmd.Flags().StringP("project", "p", "", "Project path")
	indexRelevantCmd.Flags().IntP("limit", "l", 10, "Maximum files to show")

	indexCmd.AddCommand(indexBuildCmd, indexQueryCmd, indexDepsCmd, indexRelevantCmd)

	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd, viewCmd, changelogCmd, migrateCmd, indexCmd)
}
