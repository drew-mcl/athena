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
  Goal     □  - Strategic objectives (no worktree)     [blue]
  Feature  ◇  - PR-sized work (has worktree)           [green]
  Task     ○  - Individual work items                   [yellow]

Shorthand Commands:
  g  → goal      t  → tsk       tr → tree
  f  → feat      w  → wt        q  → queue
  s  → sp        p  → plugin

Display:
  Shape colors indicate item type (blue/green/yellow)
  Filled shapes (■ ◆ ●) indicate in_progress status
  Dimmed/gray indicates completed items
  IDs shown in magenta, text in white

Examples:
  ath                               # Status summary
  ath spawn -f wi-a3f8.1            # Spawn agent on feature (primary)
  ath spawn -f wi-a3f8.1 --headless # Fire-and-forget on feature
  ath spawn                         # Interactive Claude in current dir
  ath spawn ENG-123                 # Spawn with ticket context
  ath tr                            # Full work item tree
  ath g new "Auth system"           # Create goal
  ath f new wi-a3f8 "OAuth"         # Create feature under goal
  ath t "Update readme"             # Quick task (inbox)
  ath q                             # Show merge queue`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

// Goal commands
var goalCmd = &cobra.Command{
	Use:     "goal",
	Aliases: []string{"g"},
	Short:   "Manage goals (strategic objectives)",
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
	Use:     "feat",
	Aliases: []string{"f"},
	Short:   "Manage features (PR-sized work with worktree)",
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
	Use:     "tsk [subject...]",
	Aliases: []string{"t", "task"},
	Short:   "Manage tasks",
	Long: `Create and manage tasks.

With no args: list tasks
With subject(s): create quick task(s) in inbox
With --interactive: interactive mode
With --all: list tasks across all projects

Flags:
  -f <feature-id>  Add tasks under a specific feature
  -a, --all        List tasks across all projects
  -i, --interactive  Interactive mode
  -t <type>        Filter by type (goal, feat, task)

Examples:
  ath tsk                           # List tasks
  ath tsk --interactive             # Interactive
  ath tsk "Update readme"           # Inbox task
  ath tsk -f wi-a3f8.1 "Task one"   # Under feature`,
	RunE: func(cmd *cobra.Command, args []string) error {
		featureID, _ := cmd.Flags().GetString("feature")
		itemType, _ := cmd.Flags().GetString("type")
		interactive, _ := cmd.Flags().GetBool("interactive")
		allProjects, _ := cmd.Flags().GetBool("all")

		if len(args) == 0 {
			if interactive && featureID == "" {
				// Interactive mode
				return runTskInteractive()
			}
			// List tasks
			return runTskList(itemType, allProjects)
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
	Use:     "tree [root-id]",
	Aliases: []string{"tr"},
	Short:   "Display work item tree",
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
	Use:     "wt",
	Aliases: []string{"w"},
	Short:   "Manage worktrees",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWtList()
	},
}

var wtPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Clean up merged and orphaned worktrees",
	Long: `Prune worktrees that are no longer needed:
- Merged worktrees (branch deleted from remote)
- Orphaned directories (not tracked by git)
- Stale database entries (paths that don't exist)

Examples:
  ath wt prune              # Prune all worktrees`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWtPrune()
	},
}

// Queue commands - manage the merge queue
var queueCmd = &cobra.Command{
	Use:     "queue",
	Aliases: []string{"q"},
	Short:   "Manage the merge queue",
	Long: `Manage the local merge queue for coordinating feature branches.

The merge queue maintains ordering between in-flight features so that:
- New features start from the integration HEAD (front stable queue node)
- When you edit an earlier feature, it keeps position and dependents diverge/reconcile
- Features merge to main in order

Examples:
  ath queue                    # Show queue status
  ath queue add                # Add current worktree to queue
  ath queue head               # Show integration HEAD for new features
  ath queue bump               # Refresh current queue node and reconcile dependents
  ath queue rm                 # Remove current worktree from queue`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		return runQueueList(project)
	},
}

var queueAddCmd = &cobra.Command{
	Use:   "add [worktree-path]",
	Short: "Add worktree to the merge queue",
	Long:  `Add a worktree to the merge queue. Uses current directory if no path provided.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		return runQueueAdd(path)
	},
}

var queueHeadCmd = &cobra.Command{
	Use:   "head",
	Short: "Show integration HEAD for new features",
	Long:  `Show the branch/commit that new worktrees should be based on.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		return runQueueHead(project)
	},
}

var queueBumpCmd = &cobra.Command{
	Use:   "bump [worktree-path]",
	Short: "Move worktree to back of queue",
	Long: `Move a worktree to the back of the queue after making edits.
This marks dependent features as needing rebase.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		return runQueueBump(path)
	},
}

var queueRmCmd = &cobra.Command{
	Use:   "rm [worktree-path]",
	Short: "Remove worktree from the queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		return runQueueRemove(path)
	},
}

// Plugin commands - manage integrations
var pluginCmd = &cobra.Command{
	Use:     "plugin",
	Aliases: []string{"p"},
	Short:   "Manage plugins (VCS, PM integrations)",
	Long: `Manage plugin integrations for version control and project management.

Plugin Categories:
  vcs  - Version Control Systems (github, gitlab)
  pm   - Project Management (linear, jira)

Examples:
  ath plugin                    # List all plugins
  ath plugin enable github      # Enable GitHub integration
  ath plugin disable jira       # Disable Jira
  ath plugin vcs                # List VCS plugins only`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPluginList("")
	},
}

var pluginEnableCmd = &cobra.Command{
	Use:   "enable <plugin>",
	Short: "Enable a plugin",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPluginEnable(args[0])
	},
}

var pluginDisableCmd = &cobra.Command{
	Use:   "disable <plugin>",
	Short: "Disable a plugin",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPluginDisable(args[0])
	},
}

var pluginVCSCmd = &cobra.Command{
	Use:   "vcs",
	Short: "List VCS plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPluginList("vcs")
	},
}

var pluginPMCmd = &cobra.Command{
	Use:   "pm",
	Short: "List PM plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPluginList("pm")
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
	tskCmd.Flags().BoolP("interactive", "i", false, "Interactive mode")
	tskCmd.Flags().BoolP("all", "a", false, "List tasks across all projects")
	tskCmd.Flags().StringP("type", "t", "", "Filter by type: goal, feat, task")
	tskCmd.AddCommand(tskReadyCmd)

	// Tree flags
	treeCmd.Flags().Bool("goals", false, "Show goals only")
	treeCmd.Flags().StringP("project", "p", "", "Filter by project")

	// Worktree subcommands
	wtCmd.AddCommand(wtPruneCmd)

	// Spawn flags
	spawnCmd.Flags().StringP("feature", "f", "", "Feature work item ID to spawn on")
	spawnCmd.Flags().BoolP("retrieve", "r", false, "Break down goal into features first")
	spawnCmd.Flags().Bool("headless", false, "Run agent headless in background")
	spawnCmd.Flags().BoolP("worktree", "w", false, "Create a dedicated worktree")
	spawnCmd.Flags().BoolP("parallel", "p", false, "Enable parallel task-worker mode")

	// Scratchpad flags
	spCmd.Flags().BoolP("edit", "e", false, "Open scratchpad in editor")

	// Queue flags
	queueCmd.Flags().StringP("project", "p", "", "Filter by project")
	queueHeadCmd.Flags().StringP("project", "p", "", "Project name")
	queueCmd.AddCommand(queueAddCmd, queueHeadCmd, queueBumpCmd, queueRmCmd)

	// Plugin commands
	pluginCmd.AddCommand(pluginEnableCmd, pluginDisableCmd, pluginVCSCmd, pluginPMCmd)

	rootCmd.AddCommand(goalCmd, featCmd, tskCmd, treeCmd, wtCmd, spawnCmd, spCmd, queueCmd, pluginCmd)
}

// Spawn command - unified agent launch
var spawnCmd = &cobra.Command{
	Use:   "spawn [id]",
	Short: "Spawn a Claude Code agent",
	Long: `Spawn a Claude Code agent on a feature, ticket, or work item.

Primary flow (feature):
  ath spawn -f <feature-id>     # Create worktree, task list, launch agent

Other modes:
  ath spawn                     # Interactive Claude Code in current dir
  ath spawn ENG-123             # Lookup ticket, create goal, spawn
  ath spawn wi-a3f8             # Spawn against existing work item

Modes:
  (default)     Interactive - Claude Code opens in your terminal
  --headless    Headless - agent runs in background autonomously

Flags:
  -f, --feature    Feature work item ID to spawn on (primary flow)
  -r, --retrieve   Break down the goal into features before implementing
  -w, --worktree   Create a dedicated worktree
  -p, --parallel   Enable parallel task-worker mode

Examples:
  ath spawn -f wi-a3f8.1           # Spawn on feature (creates worktree)
  ath spawn -f wi-a3f8.1 --headless # Fire-and-forget on feature
  ath spawn                        # Interactive in current dir
  ath spawn ENG-123                # Lookup ticket, spawn interactive
  ath spawn -r ENG-123             # Break down first, then implement`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		featureID, _ := cmd.Flags().GetString("feature")
		retrieve, _ := cmd.Flags().GetBool("retrieve")
		headless, _ := cmd.Flags().GetBool("headless")
		worktree, _ := cmd.Flags().GetBool("worktree")
		parallel, _ := cmd.Flags().GetBool("parallel")

		id := ""
		if len(args) > 0 {
			id = args[0]
		}

		return runSpawn(featureID, id, retrieve, headless, worktree, parallel)
	},
}

// Scratchpad command
var spCmd = &cobra.Command{
	Use:     "sp [text...]",
	Aliases: []string{"s"},
	Short:   "Scratchpad for quick ideas",
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
