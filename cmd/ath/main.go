// Command ath is a CLI for work item management and agent interaction.
package main

import (
	"fmt"
	"os"

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

Work Item Hierarchy:
  Goal     □  - Strategic objectives (no worktree)     [blue]
  Feature  ◇  - PR-sized work (has worktree)           [green]
  Task     ○  - Individual work items                   [yellow]

Shorthand Commands:
  g  → goal      t  → tsk       tr → tree
  f  → feat      w  → wt        q  → queue
  a  → agent     i  → interactive
  p  → plugin    tidy → repo maintenance

Display:
  Shape colors indicate item type (blue/green/yellow)
  Filled shapes (■ ◆ ●) indicate in_progress status
  Dimmed/gray indicates completed items
  IDs shown in magenta, text in white

Examples:
  ath                               # Status summary
  ath i                             # Interactive agent in current dir
  ath spawn -f wi-a3f8.1            # Spawn agent on feature (primary)
  ath spawn -f wi-a3f8.1 --headless # Fire-and-forget on feature
  ath spawn ENG-123                 # Spawn with ticket context
  ath agent                         # List running agents
  ath agent <id>                    # Show agent detail + session ID
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
	Long: `Manage goals - top-level strategic objectives that organize features.

Goals sit at the top of the work item hierarchy (Goal > Feature > Task).
They don't have worktrees themselves; instead, features under them do.

With no subcommand: list all goals in the current project.

Examples:
  ath goal                          # List all goals
  ath goal new "Auth system"        # Create a new goal
  ath goal new "API v2" -p myproj   # Create goal in specific project
  ath goal show wi-a3f8             # Show goal and its children`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGoalList()
	},
}

var goalNewCmd = &cobra.Command{
	Use:   "new <subject>",
	Short: "Create a new goal",
	Long: `Create a new goal (strategic objective).

The subject should be a short description of the objective.
Use -d for a longer description and -p to specify a project.

Examples:
  ath goal new "Auth system"
  ath goal new "API v2" -d "Complete REST API redesign" -p myproj`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")
		project, _ := cmd.Flags().GetString("project")
		return runGoalNew(args[0], description, project)
	},
}

var goalShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show goal details and children",
	Long: `Show a goal and its full subtree (features and tasks).

The ID should be a work item ID (e.g., wi-a3f8).

Examples:
  ath goal show wi-a3f8`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGoalShow(args[0])
	},
}

// Feature commands
var featCmd = &cobra.Command{
	Use:     "feat",
	Aliases: []string{"f"},
	Short:   "Manage features (PR-sized work with worktree)",
	Long: `Manage features - PR-sized units of work, optionally nested under goals.

Each feature gets its own worktree and can be spawned on by an agent.

With no subcommand: list all features in the current project.

Examples:
  ath feat                                # List all features
  ath feat new "OAuth flow"               # Create standalone feature
  ath feat new wi-a3f8 "OAuth flow"       # Create feature under goal
  ath feat new "Login" -t ENG-42          # Create with ticket link`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFeatList()
	},
}

var featNewCmd = &cobra.Command{
	Use:   "new [parent-id] <subject>",
	Short: "Create a new feature, optionally under a goal",
	Long: `Create a new feature, optionally nested under a goal.

If parent-id is omitted, tries to use the most recently created goal from context.
If no context exists, creates a standalone feature (no parent goal).

With one arg:  uses goal from context, or creates standalone feature
With two args: creates feature under specified goal (parent-id + subject)

Use -t to link an external ticket (Linear, Jira) and -d for a description.

Examples:
  ath feat new "OAuth flow"                         # Standalone or use context
  ath feat new wi-a3f8 "OAuth flow"                 # Explicit parent goal
  ath feat new "Login page" -t ENG-123 -d "OAuth2"  # With ticket link`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var parentID, subject string
		if len(args) == 2 {
			parentID = args[0]
			subject = args[1]
		} else {
			// No parent ID provided - will be read from context
			subject = args[0]
		}
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
	Long: `Show tasks that are unblocked and ready to be worked on.

These are tasks with no pending dependencies - they can be picked up
immediately by an agent or worked on manually.

Examples:
  ath tsk ready`,
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
	Long: `Manage git worktrees tracked by Athena.

Shows worktrees for the current project with branch info, ahead/behind
counts, and merge queue position.

With no subcommand: list all worktrees.

Examples:
  ath wt                # List worktrees with status
  ath wt prune          # Clean up merged/orphaned worktrees`,
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

var queueReconcileCmd = &cobra.Command{
	Use:     "reconcile",
	Aliases: []string{"r"},
	Short:   "Reconcile diverged queue items",
	Long: `Detect and rebase diverged items in the merge queue.

This refreshes the queue graph and automatically rebases any items
whose base commit doesn't match their predecessor's head.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		return runQueueReconcile(project)
	},
}

var queueRmCmd = &cobra.Command{
	Use:   "rm [worktree-path]",
	Short: "Remove worktree from the queue",
	Long: `Remove a worktree from the merge queue. Uses current directory if no path provided.

Examples:
  ath queue rm                    # Remove current worktree from queue
  ath queue rm ../athena-feature  # Remove specific worktree`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		return runQueueRemove(path)
	},
}

var queueGraphCmd = &cobra.Command{
	Use:     "graph",
	Aliases: []string{"g"},
	Short:   "Visual pipeline view of the merge queue",
	Long: `Show a visual graph of the merge queue pipeline.

Displays the dependency chain from main through each queued feature,
including status indicators for diverged or conflicting items.

Examples:
  ath queue graph                 # Graph for current project
  ath queue graph -p myproj       # Graph for specific project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		return runQueueGraph(project)
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

// Agent commands
var agentCmd = &cobra.Command{
	Use:     "agent [id]",
	Aliases: []string{"a"},
	Short:   "List and inspect agents",
	Long: `List running agents and inspect their details.

With no args: list all agents (most recent first)
With ID:      show agent detail including session ID

The session ID can be used with 'claude --resume <session-id>' to
attach to the agent's Claude Code session interactively.

Flags:
  -s, --status   Filter by status (running, completed, crashed, etc.)

Examples:
  ath agent                    # List all agents
  ath agent -s running         # List running agents only
  ath agent a3f8b2c1           # Show agent detail (prefix match)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return runAgentShow(args[0])
		}
		status, _ := cmd.Flags().GetString("status")
		return runAgentList(status)
	},
}

// Enable/Disable hooks
var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Install Athena lifecycle hooks into Claude Code",
	Long: `Install Athena hooks into .claude/settings.json for the current project.

Hooks fire on SessionStart, Stop, and SessionEnd to automate:
  - Auto-adding feature worktrees to the merge queue
  - Marking work items as in_progress
  - Checking PR status and updating completion state

Existing hooks (e.g., entire) are preserved.

Examples:
  ath enable       # Install hooks in current project
  ath disable      # Remove hooks`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEnable()
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove Athena lifecycle hooks from Claude Code",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDisable()
	},
}

// Hooks command (hidden) - internal plumbing called by Claude Code
var hooksCmd = &cobra.Command{
	Use:    "hooks",
	Short:  "Handle Claude Code lifecycle events (internal)",
	Hidden: true,
}

var hooksSessionStartCmd = &cobra.Command{
	Use:   "session-start",
	Short: "Handle session start event",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHookSessionStart()
	},
}

var hooksStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Handle stop event",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHookStop()
	},
}

var hooksSessionEndCmd = &cobra.Command{
	Use:   "session-end",
	Short: "Handle session end event",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHookSessionEnd()
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
	spawnCmd.Flags().StringP("archetype", "a", "", "Agent archetype (executor, planner, reconciler, ...)")

	// Run flags
	runCmd.Flags().Bool("once", false, "Run one task and stop")
	runCmd.Flags().StringP("project", "p", "", "Project name")
	runCmd.AddCommand(runStatusCmd, runStopCmd)

	// Tidy flags
	tidyCmd.Flags().Bool("headless", false, "Run reconciler in background")

	// Map flags
	mapCmd.Flags().Bool("headless", false, "Run mapper in background")

	// Queue flags
	queueCmd.Flags().StringP("project", "p", "", "Filter by project")
	queueHeadCmd.Flags().StringP("project", "p", "", "Project name")
	queueGraphCmd.Flags().StringP("project", "p", "", "Filter by project")
	queueReconcileCmd.Flags().StringP("project", "p", "", "Project name")
	queueCmd.AddCommand(queueAddCmd, queueHeadCmd, queueBumpCmd, queueRmCmd, queueGraphCmd, queueReconcileCmd)

	// Agent flags
	agentCmd.Flags().StringP("status", "s", "", "Filter by status (running, completed, crashed)")

	// Plugin commands
	pluginCmd.AddCommand(pluginEnableCmd, pluginDisableCmd, pluginVCSCmd, pluginPMCmd)

	// Hooks subcommands (hidden, internal plumbing)
	hooksCmd.AddCommand(hooksSessionStartCmd, hooksStopCmd, hooksSessionEndCmd)

	rootCmd.AddCommand(goalCmd, featCmd, tskCmd, treeCmd, wtCmd, spawnCmd, queueCmd, pluginCmd, agentCmd, interactiveCmd, tidyCmd, mapCmd, runCmd, rateCmd, enableCmd, disableCmd, hooksCmd)
}

// Spawn command - unified agent launch
var spawnCmd = &cobra.Command{
	Use:   "spawn [id]",
	Short: "Spawn a Claude Code agent",
	Long: `Spawn a Claude Code agent on a feature, ticket, or work item.

Primary flow (feature):
  ath spawn -f <feature-id>     # Create worktree, task list, launch agent
  ath spawn -w                  # Use feature from context (auto-linked)

Other modes:
  ath spawn                     # Interactive Claude Code in current dir
  ath spawn ENG-123             # Lookup ticket, create goal, spawn
  ath spawn wi-a3f8             # Spawn against existing work item

Auto-linking workflow:
  ath goal new "Build auth"     # Creates goal, saves to context
  ath feat new "OAuth login"    # Uses goal from context, saves feature
  ath spawn -w                  # Uses feature from context, creates worktree

Modes:
  (default)     Interactive - Claude Code opens in your terminal
  --headless    Headless - agent runs in background autonomously

Flags:
  -f, --feature    Feature work item ID to spawn on (primary flow)
  -r, --retrieve   Break down the goal into features before implementing
  -w, --worktree   Create a dedicated worktree (reads feature from context if no -f)
  -p, --parallel   Enable parallel task-worker mode

Examples:
  ath spawn -f wi-a3f8.1           # Spawn on feature (creates worktree)
  ath spawn -w                     # Spawn using feature from context
  ath spawn -f wi-a3f8.1 --headless # Fire-and-forget on feature
  ath spawn                        # Interactive in current dir
  ath spawn ENG-123                # Lookup ticket, spawn interactive
  ath spawn -r ENG-123             # Break down first, then implement`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		featureID, _ := cmd.Flags().GetString("feature")
		archetype, _ := cmd.Flags().GetString("archetype")
		retrieve, _ := cmd.Flags().GetBool("retrieve")
		headless, _ := cmd.Flags().GetBool("headless")
		worktree, _ := cmd.Flags().GetBool("worktree")
		parallel, _ := cmd.Flags().GetBool("parallel")

		id := ""
		if len(args) > 0 {
			id = args[0]
		}

		return runSpawn(featureID, id, archetype, retrieve, headless, worktree, parallel)
	},
}

// Interactive command - spawn an interactive agent in the current directory
var interactiveCmd = &cobra.Command{
	Use:     "i",
	Aliases: []string{"interactive"},
	Short:   "Start an interactive agent in the current directory",
	Long: `Start an interactive Claude Code session in the current directory.

This is a shorthand for 'ath spawn' with no arguments. It launches
Claude Code in your terminal with Athena context (work items, tasks).

Examples:
  ath i                           # Start interactive session
  ath interactive                 # Same thing, long form`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSpawn("", "", "", false, false, false, false)
	},
}

// Map command - run codebase mapper
var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Update codebase map (explore and document project structure)",
	Long: `Run a mapper agent that explores the codebase and updates docs/CODEBASE_MAP.md.

The mapper will:
- Scan all directories and key files
- Document package purposes and relationships
- Identify key types, interfaces, and entry points
- Update the codebase map with current information

Examples:
  ath map              # Interactive mapper
  ath map --headless   # Run mapper in background`,
	RunE: func(cmd *cobra.Command, args []string) error {
		headless, _ := cmd.Flags().GetBool("headless")
		return runMap(headless)
	},
}

// Tidy command - run repo maintenance via reconciler archetype
var tidyCmd = &cobra.Command{
	Use:   "tidy",
	Short: "Run repo maintenance (merge PRs, prune worktrees, reconcile queue)",
	Long: `Run automated repository maintenance using a reconciler agent.

The reconciler will:
- Survey branches and open PRs
- Merge approved, CI-passing PRs
- Close stale PRs (14+ days inactive, failing CI)
- Prune merged/orphaned worktrees and branches
- Fix simple CI issues (formatting, go mod tidy)
- Reconcile the merge queue

Examples:
  ath tidy              # Interactive reconciler
  ath tidy --headless   # Run reconciler in background`,
	RunE: func(cmd *cobra.Command, args []string) error {
		headless, _ := cmd.Flags().GetBool("headless")
		return runTidy(headless)
	},
}

// Run command - auto-run loop that picks tasks and spawns agents
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Auto-run loop: pick tasks, spawn agents, repeat",
	Long: `Start the auto-run loop that automatically picks ready tasks and spawns agents.

The loop:
1. Finds the next ready work item (features first, then tasks)
2. Spawns a headless agent to work on it
3. Waits for the agent to complete
4. Marks the item done (or failed)
5. Picks the next item and repeats

Use --once to run just one task. Use 'ath run stop' to halt the loop.

Examples:
  ath run                # Start auto-run loop
  ath run --once         # Run one task and stop
  ath run status         # Check auto-run status
  ath run stop           # Stop the loop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		once, _ := cmd.Flags().GetBool("once")
		project, _ := cmd.Flags().GetString("project")
		return runAutoRun(project, once)
	},
}

// Rate limit command
var rateCmd = &cobra.Command{
	Use:   "rate",
	Short: "Show API rate limit status",
	Long: `Show the current API rate limit status.

When agents share an API key, hitting a rate limit pauses all agents
until the limit resets. This command shows whether a rate limit is active.

Examples:
  ath rate                    # Show current rate limit status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRateStatus()
	},
}

var runStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show auto-run status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutoRunStatus()
	},
}

var runStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the auto-run loop",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutoRunStop()
	},
}
