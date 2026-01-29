// Command ath is a CLI for quick work item management.
// Unlike 'athena' (TUI), 'ath' is optimized for fast terminal commands.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/control"
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

func getClient() (*control.Client, error) {
	return control.NewClient(cfg.Daemon.Socket)
}

var rootCmd = &cobra.Command{
	Use:   "ath",
	Short: "Quick CLI for Athena work items",
	Long: `ath - Fast work item management for Athena.

Use 'athena' for the full TUI dashboard.
Use 'ath' for quick CLI commands.

Work Item Hierarchy:
  Goal     - Strategic objectives (no worktree)
  Feature  - PR-sized work (has worktree, Claude task list)
  Task     - Individual work items (Claude tasks)

Status indicators:
  Outline shapes (pending):  [] <> //
  Filled shapes (in_progress): [*] <*> /*/
  Dimmed (completed)

Examples:
  ath                           # Status summary
  ath tree                      # Full work item tree
  ath goal new "Auth system"    # Create goal
  ath feat new wi-a3f8 "OAuth"  # Create feature under goal
  ath tsk "Update readme"       # Quick task (inbox)
  ath tsk -f wi-a3f8.1 "Task"   # Task under feature`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

// Goal commands
var goalCmd = &cobra.Command{
	Use:   "goal",
	Short: "Manage goals (strategic objectives)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGoalList()
	},
}

var goalNewCmd = &cobra.Command{
	Use:   "new <subject>",
	Short: "Create a new goal",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")
		project, _ := cmd.Flags().GetString("project")
		return runGoalNew(args[0], description, project)
	},
}

var goalShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show goal details and children",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGoalShow(args[0])
	},
}

// Feature commands
var featCmd = &cobra.Command{
	Use:   "feat",
	Short: "Manage features (PR-sized work with worktree)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFeatList()
	},
}

var featNewCmd = &cobra.Command{
	Use:   "new <parent-id> <subject>",
	Short: "Create a new feature under a goal",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		parentID := args[0]
		subject := args[1]
		ticket, _ := cmd.Flags().GetString("ticket")
		description, _ := cmd.Flags().GetString("description")
		return runFeatNew(parentID, subject, ticket, description)
	},
}

// Task commands
var tskCmd = &cobra.Command{
	Use:   "tsk [subject...]",
	Short: "Manage tasks",
	Long: `Create and manage tasks.

With no args: interactive mode
With subject(s): create quick task(s) in inbox

Flags:
  -f <feature-id>  Add tasks under a specific feature
  -t <type>        Filter by type (goal, feat, task)

Examples:
  ath tsk                           # Interactive
  ath tsk "Update readme"           # Inbox task
  ath tsk -f wi-a3f8.1 "Task one"   # Under feature`,
	RunE: func(cmd *cobra.Command, args []string) error {
		featureID, _ := cmd.Flags().GetString("feature")
		itemType, _ := cmd.Flags().GetString("type")

		if len(args) == 0 && featureID == "" {
			// Interactive mode
			return runTskInteractive()
		}

		if len(args) == 0 {
			// List tasks
			return runTskList(itemType)
		}

		// Create task(s)
		return runTskCreate(featureID, args)
	},
}

var tskReadyCmd = &cobra.Command{
	Use:   "ready",
	Short: "Show unblocked tasks ready to work on",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTskReady()
	},
}

// Tree command
var treeCmd = &cobra.Command{
	Use:   "tree [root-id]",
	Short: "Display work item tree",
	Long: `Display hierarchical tree of work items.

Examples:
  ath tree              # Full tree
  ath tree wi-a3f8      # Subtree from ID
  ath tree --goals      # Goals only`,
	RunE: func(cmd *cobra.Command, args []string) error {
		goalsOnly, _ := cmd.Flags().GetBool("goals")
		project, _ := cmd.Flags().GetString("project")

		rootID := ""
		if len(args) > 0 {
			rootID = args[0]
		}
		return runTree(rootID, project, goalsOnly)
	},
}

// Worktree commands
var wtCmd = &cobra.Command{
	Use:   "wt",
	Short: "Manage worktrees",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWtList()
	},
}

func init() {
	// Goal flags
	goalNewCmd.Flags().StringP("description", "d", "", "Goal description")
	goalNewCmd.Flags().StringP("project", "p", "", "Project name")
	goalCmd.AddCommand(goalNewCmd, goalShowCmd)

	// Feature flags
	featNewCmd.Flags().StringP("ticket", "t", "", "External ticket ID (e.g., ENG-123)")
	featNewCmd.Flags().StringP("description", "d", "", "Feature description")
	featCmd.AddCommand(featNewCmd)

	// Task flags
	tskCmd.Flags().StringP("feature", "f", "", "Parent feature ID")
	tskCmd.Flags().StringP("type", "t", "", "Filter by type: goal, feat, task")
	tskCmd.AddCommand(tskReadyCmd)

	// Tree flags
	treeCmd.Flags().Bool("goals", false, "Show goals only")
	treeCmd.Flags().StringP("project", "p", "", "Filter by project")

	// Scratchpad flags
	spCmd.Flags().BoolP("edit", "e", false, "Open scratchpad in editor")

	rootCmd.AddCommand(goalCmd, featCmd, tskCmd, treeCmd, wtCmd, spCmd)
}

// Scratchpad command
var spCmd = &cobra.Command{
	Use:   "sp [text...]",
	Short: "Scratchpad for quick ideas",
	Long: `Quick scratchpad for jotting down ideas.

With no args: show all scratchpad entries
With text: add a new entry

Examples:
  ath sp                     # Show scratchpad
  ath sp "my idea here"      # Add entry
  ath sp -e                  # Open in editor (coming soon)

Entries can be multi-line. Use quotes for text with spaces.
Later, use an agent to organize entries into goals/features.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runSpList()
		}
		// Join all args as one entry
		text := strings.Join(args, " ")
		return runSpAdd(text)
	},
}
