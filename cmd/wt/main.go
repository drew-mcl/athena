// Command wt manages git worktrees across projects.
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/worktree"
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
	Use:   "wt",
	Short: "Manage git worktrees across projects",
	Long: `wt is a worktree management tool for multi-project development.

It discovers git worktrees from configured directories and provides
commands to list, add, remove, and prune worktrees.`,
	RunE: runList,
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all worktrees",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList(cmd, args)
	},
}

var addCmd = &cobra.Command{
	Use:   "add <project> <name>",
	Short: "Add a new worktree",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAdd(args[0], args[1])
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove <path>",
	Aliases: []string{"rm"},
	Short:   "Remove a worktree",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRemove(args[0])
	},
}

var pruneCmd = &cobra.Command{
	Use:   "prune [project]",
	Short: "Prune merged worktrees",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project := ""
		if len(args) > 0 {
			project = args[0]
		}
		return runPrune(project)
	},
}

func init() {
	rootCmd.AddCommand(listCmd, addCmd, removeCmd, pruneCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	st, err := store.New(cfg.Daemon.Database)
	if err != nil {
		return err
	}
	defer st.Close()

	// Scan first to get fresh data
	scanner := worktree.NewScanner(cfg, st)
	scanner.ScanAndStore()

	worktrees, err := st.ListWorktrees("")
	if err != nil {
		return err
	}

	if len(worktrees) == 0 {
		fmt.Println("No worktrees found.")
		fmt.Printf("Configure base_dirs in %s\n", config.DefaultConfigPath())
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tPATH\tBRANCH\tMAIN\tAGENT")
	fmt.Fprintln(w, "-------\t----\t------\t----\t-----")

	for _, wt := range worktrees {
		isMain := ""
		if wt.IsMain {
			isMain = "*"
		}
		agentID := ""
		if wt.AgentID != nil {
			agentID = (*wt.AgentID)[:8] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			wt.Project, wt.Path, wt.Branch, isMain, agentID)
	}
	w.Flush()
	return nil
}

func runAdd(project, name string) error {
	st, err := store.New(cfg.Daemon.Database)
	if err != nil {
		return err
	}
	defer st.Close()

	provisioner := worktree.NewProvisioner(cfg, st)

	// Create a temporary job to generate the worktree
	job := &store.Job{
		ID:              name,
		RawInput:        name,
		NormalizedInput: name,
	}

	wt, err := provisioner.CreateForJob(project, job)
	if err != nil {
		return err
	}

	fmt.Printf("Created worktree: %s\n", wt.Path)
	fmt.Printf("Branch: %s\n", wt.Branch)
	return nil
}

func runRemove(path string) error {
	st, err := store.New(cfg.Daemon.Database)
	if err != nil {
		return err
	}
	defer st.Close()

	provisioner := worktree.NewProvisioner(cfg, st)

	if err := provisioner.Remove(path, true); err != nil {
		return err
	}

	fmt.Printf("Removed worktree: %s\n", path)
	return nil
}

func runPrune(project string) error {
	st, err := store.New(cfg.Daemon.Database)
	if err != nil {
		return err
	}
	defer st.Close()

	provisioner := worktree.NewProvisioner(cfg, st)

	// Get all projects if none specified
	var projects []string
	if project != "" {
		projects = []string{project}
	} else {
		projects, err = st.GetProjectNames()
		if err != nil {
			return err
		}
	}

	total := 0
	for _, p := range projects {
		pruned, err := provisioner.Prune(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error pruning %s: %v\n", p, err)
			continue
		}
		for _, path := range pruned {
			fmt.Printf("Pruned: %s\n", path)
			total++
		}
	}

	if total == 0 {
		fmt.Println("No merged worktrees to prune.")
	} else {
		fmt.Printf("Pruned %d worktree(s).\n", total)
	}
	return nil
}
