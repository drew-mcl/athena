package daemon

import (
	"strings"
	"testing"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/store"
)

func TestBuildSpawnPrompt(t *testing.T) {
	d := &Daemon{
		config: &config.Config{},
	}

	t.Run("BareTask", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-a1b2",
			Project:  "myproject",
			ItemType: store.WorkItemTypeTask,
			Subject:  "Interactive session",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-a1b2", false)

		// Should contain project
		if !strings.Contains(prompt, "myproject") {
			t.Error("expected prompt to contain project name")
		}

		// Should contain task list ID
		if !strings.Contains(prompt, "`wi-a1b2`") {
			t.Error("expected prompt to contain task list ID")
		}

		// Should suppress "Interactive session" subject
		if strings.Contains(prompt, "**Subject:** Interactive session") {
			t.Error("expected prompt to suppress Interactive session subject")
		}

		// Should NOT contain goal or feature sections
		if strings.Contains(prompt, "## Goal") {
			t.Error("expected prompt to NOT contain goal section")
		}
		if strings.Contains(prompt, "## Feature") {
			t.Error("expected prompt to NOT contain feature section")
		}

		// Should NOT contain retrieve mode instructions
		if strings.Contains(prompt, "Mode: Retrieve") {
			t.Error("expected prompt to NOT contain retrieve mode")
		}
	})

	t.Run("FeatureWithParentGoal", func(t *testing.T) {
		wtPath := "/home/user/repos/worktrees/myproject/wi-a1b2"
		parentGoal := &store.WorkItem{
			ID:          "wi-c3d4",
			Project:     "myproject",
			ItemType:    store.WorkItemTypeGoal,
			Subject:     "Add dark mode",
			Description: "Implement dark mode for the UI",
		}
		wi := &store.WorkItem{
			ID:           "wi-c3d4.1",
			Project:      "myproject",
			ItemType:     store.WorkItemTypeFeature,
			Subject:      "Dark mode toggle",
			Description:  "Add toggle switch to settings",
			WorktreePath: &wtPath,
		}

		prompt := d.buildSpawnPrompt(wi, parentGoal, "", "wi-c3d4.1", false)

		// Should contain goal section
		if !strings.Contains(prompt, "## Goal") {
			t.Error("expected prompt to contain goal section")
		}
		if !strings.Contains(prompt, "Add dark mode") {
			t.Error("expected prompt to contain goal subject")
		}
		if !strings.Contains(prompt, "Implement dark mode") {
			t.Error("expected prompt to contain goal description")
		}

		// Should contain feature section
		if !strings.Contains(prompt, "## Feature") {
			t.Error("expected prompt to contain feature section")
		}
		if !strings.Contains(prompt, "Dark mode toggle") {
			t.Error("expected prompt to contain feature subject")
		}
		if !strings.Contains(prompt, wtPath) {
			t.Error("expected prompt to contain worktree path")
		}
	})

	t.Run("WithTicketContext", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-e5f6",
			Project:  "myproject",
			ItemType: store.WorkItemTypeGoal,
			Subject:  "Fix login bug",
		}

		ticketCtx := "## Ticket: ENG-456\n**Title:** Fix login bug\n**Description:** Users can't log in\n"

		prompt := d.buildSpawnPrompt(wi, nil, ticketCtx, "wi-e5f6", false)

		if !strings.Contains(prompt, "## Ticket: ENG-456") {
			t.Error("expected prompt to contain ticket context")
		}
		if !strings.Contains(prompt, "Users can't log in") {
			t.Error("expected prompt to contain ticket description")
		}
	})

	t.Run("RetrieveMode", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-g7h8",
			Project:  "myproject",
			ItemType: store.WorkItemTypeGoal,
			Subject:  "Refactor auth",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-g7h8", true)

		if !strings.Contains(prompt, "Mode: Retrieve & Plan") {
			t.Error("expected prompt to contain retrieve mode header")
		}
		if !strings.Contains(prompt, "Explore the codebase") {
			t.Error("expected prompt to contain retrieve instructions")
		}
	})

	t.Run("NonFeatureWithSubject", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:          "wi-i9j0",
			Project:     "myproject",
			ItemType:    store.WorkItemTypeTask,
			Subject:     "Fix broken tests",
			Description: "Unit tests are failing in CI",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-i9j0", false)

		// Non-interactive-session subject should appear
		if !strings.Contains(prompt, "**Subject:** Fix broken tests") {
			t.Error("expected prompt to contain task subject")
		}
		if !strings.Contains(prompt, "**Description:** Unit tests are failing") {
			t.Error("expected prompt to contain task description")
		}
	})

	t.Run("CompletionWorkflowInstructions", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-k1l2",
			Project:  "myproject",
			ItemType: store.WorkItemTypeFeature,
			Subject:  "Add new feature",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-k1l2", false)

		// Should contain completion workflow instructions
		if !strings.Contains(prompt, "## When Done") {
			t.Error("expected prompt to contain completion section")
		}
		if !strings.Contains(prompt, "Create a PR") {
			t.Error("expected prompt to mention creating a PR")
		}
		if !strings.Contains(prompt, "already in the merge queue") {
			t.Error("expected prompt to mention auto-queue")
		}
		if !strings.Contains(prompt, "Commit and push") {
			t.Error("expected prompt to mention committing and pushing")
		}
		if !strings.Contains(prompt, "Verify the PR") {
			t.Error("expected prompt to mention verifying the PR")
		}
	})

	t.Run("BareTaskNoWhenDone", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-m3n4",
			Project:  "myproject",
			ItemType: store.WorkItemTypeTask,
			Subject:  "Interactive session",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-m3n4", false)

		// Task work items should NOT contain the When Done section
		if strings.Contains(prompt, "## When Done") {
			t.Error("expected bare task prompt to NOT contain When Done section")
		}
		if strings.Contains(prompt, "/commit-push-pr") {
			t.Error("expected bare task prompt to NOT mention /commit-push-pr")
		}
	})

	t.Run("GoalHasWhenDone", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-o5p6",
			Project:  "myproject",
			ItemType: store.WorkItemTypeGoal,
			Subject:  "Build auth system",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-o5p6", false)

		if !strings.Contains(prompt, "## When Done") {
			t.Error("expected goal prompt to contain When Done section")
		}
	})

	t.Run("GoalHasOrchestratorGuidance", func(t *testing.T) {
		wi := &store.WorkItem{
			ID:       "wi-orch-test",
			Project:  "myproject",
			ItemType: store.WorkItemTypeGoal,
			Subject:  "Implement new feature",
		}

		prompt := d.buildSpawnPrompt(wi, nil, "", "wi-orch-test", false)

		// Should contain orchestrator-specific sections
		if !strings.Contains(prompt, "## Goal Orchestration") {
			t.Error("expected goal prompt to contain Goal Orchestration section")
		}
		if !strings.Contains(prompt, "Break Down into Features") {
			t.Error("expected goal prompt to contain feature breakdown guidance")
		}
		if !strings.Contains(prompt, "TaskCreate") {
			t.Error("expected goal prompt to mention TaskCreate for features")
		}
		if !strings.Contains(prompt, "TeamCreate") {
			t.Error("expected goal prompt to mention TeamCreate for team approach")
		}
		if !strings.Contains(prompt, "Work solo if:") {
			t.Error("expected goal prompt to contain solo approach criteria")
		}
		if !strings.Contains(prompt, "Create a team if:") {
			t.Error("expected goal prompt to contain team approach criteria")
		}
	})
}

func TestBuildInteractiveExec(t *testing.T) {
	t.Run("DefaultNoArchetype", func(t *testing.T) {
		d := &Daemon{
			config: &config.Config{
				Archetypes: map[string]config.Archetype{},
			},
		}

		args, env := d.buildInteractiveExec("test prompt", "executor", "wi-a1b2")

		// Should start with "claude"
		if len(args) == 0 || args[0] != "claude" {
			t.Fatalf("expected args[0] = 'claude', got %v", args)
		}

		// Should have --append-system-prompt with the prompt
		foundPrompt := false
		for i, arg := range args {
			if arg == "--append-system-prompt" && i+1 < len(args) && args[i+1] == "test prompt" {
				foundPrompt = true
				break
			}
		}
		if !foundPrompt {
			t.Errorf("expected --append-system-prompt 'test prompt' in args: %v", args)
		}

		// Should NOT have --model or --permission-mode (archetype not found)
		for _, arg := range args {
			if arg == "--model" || arg == "--permission-mode" {
				t.Errorf("unexpected flag %q when archetype not configured", arg)
			}
		}

		// Should have task list ID in env
		if len(env) != 1 || env[0] != "CLAUDE_CODE_TASK_LIST_ID=wi-a1b2" {
			t.Errorf("expected env [CLAUDE_CODE_TASK_LIST_ID=wi-a1b2], got %v", env)
		}
	})

	t.Run("WithArchetypeConfig", func(t *testing.T) {
		d := &Daemon{
			config: &config.Config{
				Archetypes: map[string]config.Archetype{
					"planner": {
						Model:          "opus",
						PermissionMode: "plan",
					},
				},
			},
		}

		args, env := d.buildInteractiveExec("plan this", "planner", "wi-c3d4")

		// Should contain model flag
		foundModel := false
		for i, arg := range args {
			if arg == "--model" && i+1 < len(args) && args[i+1] == "opus" {
				foundModel = true
				break
			}
		}
		if !foundModel {
			t.Errorf("expected --model opus in args: %v", args)
		}

		// Should contain permission mode flag
		foundPerm := false
		for i, arg := range args {
			if arg == "--permission-mode" && i+1 < len(args) && args[i+1] == "plan" {
				foundPerm = true
				break
			}
		}
		if !foundPerm {
			t.Errorf("expected --permission-mode plan in args: %v", args)
		}

		// Should have task list ID in env
		if len(env) != 1 || env[0] != "CLAUDE_CODE_TASK_LIST_ID=wi-c3d4" {
			t.Errorf("expected env [CLAUDE_CODE_TASK_LIST_ID=wi-c3d4], got %v", env)
		}
	})

	t.Run("ArchetypeWithOnlyModel", func(t *testing.T) {
		d := &Daemon{
			config: &config.Config{
				Archetypes: map[string]config.Archetype{
					"executor": {
						Model: "sonnet",
						// No PermissionMode
					},
				},
			},
		}

		args, _ := d.buildInteractiveExec("do work", "executor", "wi-e5f6")

		// Should have model but NOT permission mode
		foundModel := false
		foundPerm := false
		for _, arg := range args {
			if arg == "--model" {
				foundModel = true
			}
			if arg == "--permission-mode" {
				foundPerm = true
			}
		}
		if !foundModel {
			t.Error("expected --model flag")
		}
		if foundPerm {
			t.Error("did not expect --permission-mode flag when not configured")
		}
	})
}
