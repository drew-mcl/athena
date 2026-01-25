// Command athena is the Athena CLI and TUI.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/index"
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
		format, _ := cmd.Flags().GetString("format")
		exportedOnly, _ := cmd.Flags().GetBool("exported-only")
		return runIndexQuery(args[0], project, format, exportedOnly)
	},
}

var indexDepsCmd = &cobra.Command{
	Use:   "deps <file>",
	Short: "Show dependencies for a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		reverse, _ := cmd.Flags().GetBool("reverse")
		format, _ := cmd.Flags().GetString("format")
		return runIndexDeps(args[0], project, reverse, format)
	},
}

var indexRelevantCmd = &cobra.Command{
	Use:   "relevant <task>",
	Short: "Find files relevant to a task description",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")
		return runIndexRelevant(args[0], project, limit, format)
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

func runIndexQuery(symbol, projectPath, format string, exportedOnly bool) error {
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

	// Filter to exported symbols if requested
	if exportedOnly {
		filtered := make([]index.Symbol, 0, len(results))
		for _, sym := range results {
			if sym.Exported {
				filtered = append(filtered, sym)
			}
		}
		results = filtered
	}

	if len(results) == 0 {
		if format == "json" {
			fmt.Println("[]")
			return nil
		}
		fmt.Printf("No definitions found for '%s'\n", symbol)
		return nil
	}

	switch format {
	case "json":
		return formatQueryJSON(results)
	case "compact":
		return formatQueryCompact(results)
	default: // "text" or empty
		return formatQueryText(symbol, results)
	}
}

// symbolJSON is the JSON representation of a symbol for output.
type symbolJSON struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Package  string `json:"package,omitempty"`
	Receiver string `json:"receiver,omitempty"`
	Exported bool   `json:"exported"`
}

func formatQueryJSON(results []index.Symbol) error {
	output := make([]symbolJSON, 0, len(results))
	for _, sym := range results {
		output = append(output, symbolJSON{
			Name:     sym.Name,
			Kind:     string(sym.Kind),
			File:     sym.FilePath,
			Line:     sym.LineNumber,
			Package:  sym.Package,
			Receiver: sym.Receiver,
			Exported: sym.Exported,
		})
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func formatQueryCompact(results []index.Symbol) error {
	for _, sym := range results {
		if sym.Receiver != "" {
			fmt.Printf("%s.%s (%s) at %s:%d\n", sym.Receiver, sym.Name, sym.Kind, sym.FilePath, sym.LineNumber)
		} else {
			fmt.Printf("%s (%s) at %s:%d\n", sym.Name, sym.Kind, sym.FilePath, sym.LineNumber)
		}
	}
	return nil
}

func formatQueryText(symbol string, results []index.Symbol) error {
	fmt.Printf("%s (%s) defined in:\n", symbol, results[0].Kind)
	for _, sym := range results {
		fmt.Printf("  %s:%d\n", sym.FilePath, sym.LineNumber)
	}
	return nil
}

func runIndexDeps(filePath, projectPath string, reverse bool, format string) error {
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
		return formatDependentsOutput(filePath, deps, format)
	}

	deps := idx.GetDependencies(filePath)
	return formatDepsOutput(filePath, deps, format)
}

// depsJSON is the JSON representation of dependencies for output.
type depsJSON struct {
	File     string   `json:"file"`
	Imports  []string `json:"imports,omitempty"`
	External []string `json:"external,omitempty"`
}

// dependentsJSON is the JSON representation of dependents for output.
type dependentsJSON struct {
	File       string   `json:"file"`
	ImportedBy []string `json:"imported_by"`
}

func formatDepsOutput(filePath string, deps []index.Dependency, format string) error {
	if len(deps) == 0 {
		if format == "json" {
			data, _ := json.Marshal(depsJSON{File: filePath, Imports: []string{}, External: []string{}})
			fmt.Println(string(data))
			return nil
		}
		fmt.Printf("%s has no imports\n", filePath)
		return nil
	}

	switch format {
	case "json":
		return formatDepsJSON(filePath, deps)
	case "compact":
		return formatDepsCompact(filePath, deps)
	default: // "text" or empty
		return formatDepsText(filePath, deps)
	}
}

func formatDepsJSON(filePath string, deps []index.Dependency) error {
	output := depsJSON{File: filePath}
	for _, dep := range deps {
		if dep.IsInternal {
			output.Imports = append(output.Imports, dep.ToFile)
		} else {
			output.External = append(output.External, dep.ImportPath)
		}
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func formatDepsCompact(filePath string, deps []index.Dependency) error {
	for _, dep := range deps {
		if dep.IsInternal {
			fmt.Printf("%s -> %s\n", filePath, dep.ToFile)
		} else {
			fmt.Printf("%s -> %s (external)\n", filePath, dep.ImportPath)
		}
	}
	return nil
}

func formatDepsText(filePath string, deps []index.Dependency) error {
	fmt.Printf("%s imports:\n", filePath)
	for _, dep := range deps {
		if dep.IsInternal {
			fmt.Printf("  %s\n", dep.ToFile)
		} else {
			fmt.Printf("  %s (external)\n", dep.ImportPath)
		}
	}
	return nil
}

func formatDependentsOutput(filePath string, deps []string, format string) error {
	if len(deps) == 0 {
		if format == "json" {
			data, _ := json.Marshal(dependentsJSON{File: filePath, ImportedBy: []string{}})
			fmt.Println(string(data))
			return nil
		}
		fmt.Printf("No files import %s\n", filePath)
		return nil
	}

	switch format {
	case "json":
		return formatDependentsJSON(filePath, deps)
	case "compact":
		return formatDependentsCompact(filePath, deps)
	default: // "text" or empty
		return formatDependentsText(filePath, deps)
	}
}

func formatDependentsJSON(filePath string, deps []string) error {
	output := dependentsJSON{
		File:       filePath,
		ImportedBy: deps,
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func formatDependentsCompact(filePath string, deps []string) error {
	for _, dep := range deps {
		fmt.Printf("%s <- %s\n", filePath, dep)
	}
	return nil
}

func formatDependentsText(filePath string, deps []string) error {
	fmt.Printf("%s is imported by:\n", filePath)
	for _, dep := range deps {
		fmt.Printf("  %s\n", dep)
	}
	return nil
}

func runIndexRelevant(task, projectPath string, limit int, format string) error {
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
		if format == "json" {
			fmt.Println("[]")
			return nil
		}
		fmt.Printf("No relevant files found for: %s\n", task)
		return nil
	}

	switch format {
	case "json":
		return formatRelevantJSON(results, path)
	case "compact":
		return formatRelevantCompact(results, path)
	default: // "text" or empty
		return formatRelevantText(task, results)
	}
}

// relevantFileJSON is the JSON representation of a relevant file for output.
type relevantFileJSON struct {
	Path   string  `json:"path"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

func formatRelevantJSON(results []index.ScoredFile, projectPath string) error {
	output := make([]relevantFileJSON, 0, len(results))
	for _, r := range results {
		// Compute relative path for cleaner output
		relPath, err := filepath.Rel(projectPath, r.Path)
		if err != nil {
			relPath = r.Path
		}
		output = append(output, relevantFileJSON{
			Path:   relPath,
			Score:  r.Score,
			Reason: r.Reason,
		})
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func formatRelevantCompact(results []index.ScoredFile, projectPath string) error {
	for _, r := range results {
		// Compute relative path for cleaner output
		relPath, err := filepath.Rel(projectPath, r.Path)
		if err != nil {
			relPath = r.Path
		}
		fmt.Printf("%s (%.2f)\n", relPath, r.Score)
	}
	return nil
}

func formatRelevantText(task string, results []index.ScoredFile) error {
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
	indexQueryCmd.Flags().StringP("format", "f", "text", "Output format: text, json, compact")
	indexQueryCmd.Flags().Bool("exported-only", false, "Only show exported (public) symbols")

	indexDepsCmd.Flags().StringP("project", "p", "", "Project path")
	indexDepsCmd.Flags().BoolP("reverse", "r", false, "Show files that import this file")
	indexDepsCmd.Flags().StringP("format", "f", "text", "Output format: text, json, compact")

	indexRelevantCmd.Flags().StringP("project", "p", "", "Project path")
	indexRelevantCmd.Flags().IntP("limit", "l", 10, "Maximum files to show")
	indexRelevantCmd.Flags().StringP("format", "f", "text", "Output format: text, json, compact")

	indexCmd.AddCommand(indexBuildCmd, indexQueryCmd, indexDepsCmd, indexRelevantCmd)

	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd, viewCmd, changelogCmd, migrateCmd, indexCmd)
}
