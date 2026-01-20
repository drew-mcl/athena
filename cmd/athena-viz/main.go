// Command athena-viz visualizes real-time data flow through Athena components.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/viz"
	"github.com/spf13/cobra"
)

var (
	cfg       *config.Config
	agentID   string
	worktree  string
	showAll   bool
)

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
	Use:   "athena-viz",
	Short: "Real-time data flow visualization for Athena",
	Long: `Visualize data flowing through Athena components in real-time.

athena-viz connects to the daemon and displays:
- Agent messages (thinking, tool calls, responses)
- Job status changes and lifecycle events
- Store operations and API calls
- System health and metrics

Views:
  Timeline - Waterfall of events across all agents
  Flow     - Network diagram showing component connections
  Detail   - Inspector for individual events

Examples:
  athena-viz                    # All events
  athena-viz -a abc123          # Filter by agent ID
  athena-viz -w /path/to/wt     # Filter by worktree
`,
	RunE: runViz,
}

func init() {
	rootCmd.Flags().StringVarP(&agentID, "agent", "a", "", "Filter events by agent ID")
	rootCmd.Flags().StringVarP(&worktree, "worktree", "w", "", "Filter events by worktree path")
	rootCmd.Flags().BoolVarP(&showAll, "all", "A", false, "Show all event types (including internal)")
}

func runViz(cmd *cobra.Command, args []string) error {
	// Connect to daemon
	client, err := control.NewClient(cfg.Daemon.Socket)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nIs athenad running? Start it with: athenad", err)
	}
	defer client.Close()

	// Subscribe to stream with filters
	filter := control.SubscribeStreamRequest{
		AgentID:      agentID,
		WorktreePath: worktree,
	}

	resp, err := client.SubscribeStream(filter)
	if err != nil {
		return fmt.Errorf("failed to subscribe to stream: %w", err)
	}

	// Create the TUI model
	model := viz.NewModel(client, filter, resp.ActiveAgents)

	// Run the Bubble Tea program
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running visualization: %w", err)
	}

	return nil
}
