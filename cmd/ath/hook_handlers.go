package main

import (
	"fmt"
	"os"

	"github.com/drewfead/athena/internal/control"
)

// runHookSessionStart is called by Claude Code's SessionStart hook.
// Always exits 0 — failures must not block the agent.
func runHookSessionStart() error {
	workItemID := os.Getenv("CLAUDE_CODE_TASK_LIST_ID")
	if workItemID == "" {
		return nil // Not an Athena session
	}

	client, err := getClient()
	if err != nil {
		// Daemon not running — silently succeed
		fmt.Fprintf(os.Stderr, "ath hooks: daemon not available: %v\n", err)
		return nil
	}
	defer client.Close()

	workDir, _ := os.Getwd()

	resp, err := client.HookSessionStart(control.HookSessionStartRequest{
		WorkItemID: workItemID,
		WorkDir:    workDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ath hooks: session-start: %v\n", err)
		return nil // Always exit 0
	}

	if resp.Message != "" && resp.Message != "ok" {
		fmt.Fprintf(os.Stderr, "ath hooks: session-start: %s\n", resp.Message)
	}
	return nil
}

// runHookStop is called by Claude Code's Stop hook.
// Always exits 0 — failures must not block the agent.
func runHookStop() error {
	workItemID := os.Getenv("CLAUDE_CODE_TASK_LIST_ID")
	if workItemID == "" {
		return nil
	}

	client, err := getClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ath hooks: daemon not available: %v\n", err)
		return nil
	}
	defer client.Close()

	workDir, _ := os.Getwd()

	resp, err := client.HookStop(control.HookStopRequest{
		WorkItemID: workItemID,
		WorkDir:    workDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ath hooks: stop: %v\n", err)
		return nil
	}

	if resp.Message != "" && resp.Message != "ok" {
		fmt.Fprintf(os.Stderr, "ath hooks: stop: %s\n", resp.Message)
	}
	return nil
}

// runHookSessionEnd is called by Claude Code's SessionEnd hook.
// Always exits 0 — failures must not block the agent.
func runHookSessionEnd() error {
	workItemID := os.Getenv("CLAUDE_CODE_TASK_LIST_ID")
	if workItemID == "" {
		return nil
	}

	client, err := getClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ath hooks: daemon not available: %v\n", err)
		return nil
	}
	defer client.Close()

	workDir, _ := os.Getwd()

	resp, err := client.HookSessionEnd(control.HookSessionEndRequest{
		WorkItemID: workItemID,
		WorkDir:    workDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ath hooks: session-end: %v\n", err)
		return nil
	}

	if resp.Message != "" && resp.Message != "ok" {
		fmt.Fprintf(os.Stderr, "ath hooks: session-end: %s\n", resp.Message)
	}
	return nil
}
