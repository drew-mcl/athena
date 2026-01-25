package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/drewfead/athena/internal/control"
)

func main() {
	spawnCmd := flag.NewFlagSet("spawn", flag.ExitOnError)
	worktreePath := spawnCmd.String("worktree", "", "Path to the worktree")
	prompt := spawnCmd.String("prompt", "", "Prompt for the agent")
	archetype := spawnCmd.String("archetype", "executor", "Archetype (executor, planner, etc.)")
	provider := spawnCmd.String("provider", "claude", "Provider (claude, gemini)")
	
	createJobCmd := flag.NewFlagSet("create-job", flag.ExitOnError)
	jobInput := createJobCmd.String("input", "", "Job input/description")
	jobProject := createJobCmd.String("project", "", "Project name")
	jobType := createJobCmd.String("type", "feature", "Job type (feature, question, quick)")

	getAgentCmd := flag.NewFlagSet("get-agent", flag.ExitOnError)
	agentID := getAgentCmd.String("id", "", "Agent ID")

	if len(os.Args) < 2 {
		fmt.Println("expected 'spawn', 'create-job', or 'get-agent' subcommands")
		os.Exit(1)
	}

	socketPath := "/tmp/athena.sock" // Default
	if os.Getenv("ATHENA_SOCKET") != "" {
		socketPath = os.Getenv("ATHENA_SOCKET")
	}

	client, err := control.NewClient(socketPath)
	if err != nil {
		log.Fatalf("failed to connect to daemon: %v", err)
	}
	defer client.Close()

	switch os.Args[1] {
	case "spawn":
		spawnCmd.Parse(os.Args[2:])
		if *worktreePath == "" || *prompt == "" {
			fmt.Println("worktree and prompt are required")
			os.Exit(1)
		}
		
		fmt.Printf("Spawning agent on %s...\n", *worktreePath)
		start := time.Now()
		
		req := control.SpawnAgentRequest{
			WorktreePath: *worktreePath,
			Archetype:    *archetype,
			Prompt:       *prompt,
			Provider:     *provider,
		}
		
		agent, err := client.SpawnAgent(req)
		if err != nil {
			log.Fatalf("failed to spawn agent: %v", err)
		}
		
		fmt.Printf("Agent spawned successfully!\n")
		fmt.Printf("ID: %s\n", agent.ID)
		fmt.Printf("Status: %s\n", agent.Status)
		fmt.Printf("Wallclock setup time: %v\n", time.Since(start))
		
	case "create-job":
		createJobCmd.Parse(os.Args[2:])
		if *jobInput == "" || *jobProject == "" {
			fmt.Println("input and project are required")
			os.Exit(1)
		}

		fmt.Printf("Creating job for project %s...\n", *jobProject)
		req := control.CreateJobRequest{
			Input:   *jobInput,
			Project: *jobProject,
			Type:    *jobType,
		}

		job, err := client.CreateJob(req)
		if err != nil {
			log.Fatalf("failed to create job: %v", err)
		}

		fmt.Printf("Job created successfully!\n")
		fmt.Printf("ID: %s\n", job.ID)
		fmt.Printf("Status: %s\n", job.Status)

	case "get-agent":
		getAgentCmd.Parse(os.Args[2:])
		if *agentID == "" {
			fmt.Println("agent id is required")
			os.Exit(1)
		}

		agent, err := client.GetAgent(*agentID)
		if err != nil {
			log.Fatalf("failed to get agent: %v", err)
		}

		fmt.Printf("Agent ID: %s\n", agent.ID)
		fmt.Printf("Status: %s\n", agent.Status)
		if agent.Metrics != nil {
			fmt.Printf("Duration: %d ms\n", agent.Metrics.DurationMs)
			fmt.Printf("Input Tokens: %d\n", agent.Metrics.InputTokens)
			fmt.Printf("Output Tokens: %d\n", agent.Metrics.OutputTokens)
			fmt.Printf("Cache Reads: %d\n", agent.Metrics.CacheReads)
			fmt.Printf("Total Tokens: %d\n", agent.Metrics.TotalTokens)
			fmt.Printf("Tool Use Count: %d\n", agent.Metrics.ToolUseCount)
			fmt.Printf("Cost: %d cents\n", agent.Metrics.CostCents)
		} else {
			fmt.Println("No metrics available yet.")
		}
		
	default:
		fmt.Println("expected 'spawn', 'create-job', or 'get-agent' subcommands")
		os.Exit(1)
	}
}
