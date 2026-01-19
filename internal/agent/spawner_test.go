package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/store"
)

func setupTestEnv(t *testing.T) (*store.Store, *config.Config, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "athena-agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create store: %v", err)
	}

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Model: "sonnet",
		},
		Archetypes: map[string]config.Archetype{
			"planner": {
				Model:          "sonnet",
				PermissionMode: "plan",
				AllowedTools:   []string{"Glob", "Grep", "Read"},
				Prompt:         "You are a planning agent. Analyze the codebase.",
			},
			"executor": {
				Model:          "sonnet",
				PermissionMode: "default",
				AllowedTools:   nil, // All tools
				Prompt:         "You are an execution agent. Implement the plan.",
			},
		},
	}

	cleanup := func() {
		st.Close()
		os.RemoveAll(tmpDir)
	}

	return st, cfg, cleanup
}

// TestSpawnerBuildRunSpec tests that run specs are correctly built from archetypes
func TestSpawnerBuildRunSpec(t *testing.T) {
	st, cfg, cleanup := setupTestEnv(t)
	defer cleanup()

	spawner := NewSpawner(cfg, st, nil) // nil publisher for tests

	t.Run("PlannerArchetype", func(t *testing.T) {
		spec := SpawnSpec{
			WorktreePath: "/home/user/repos/project",
			ProjectName:  "project",
			Archetype:    "planner",
			Prompt:       "Create a plan for adding dark mode",
		}

		runSpec, _ := spawner.buildRunSpec(spec, "session-123")

		if runSpec.SessionID != "session-123" {
			t.Errorf("Expected session ID 'session-123', got '%s'", runSpec.SessionID)
		}

		if runSpec.WorkDir != spec.WorktreePath {
			t.Errorf("Expected work dir '%s', got '%s'", spec.WorktreePath, runSpec.WorkDir)
		}

		if runSpec.Model != "sonnet" {
			t.Errorf("Expected model 'sonnet', got '%s'", runSpec.Model)
		}

		if runSpec.PermissionMode != "plan" {
			t.Errorf("Expected permission mode 'plan', got '%s'", runSpec.PermissionMode)
		}

		if len(runSpec.AllowedTools) != 3 {
			t.Errorf("Expected 3 allowed tools, got %d", len(runSpec.AllowedTools))
		}
	})

	t.Run("ExecutorArchetype", func(t *testing.T) {
		spec := SpawnSpec{
			WorktreePath: "/home/user/repos/project",
			ProjectName:  "project",
			Archetype:    "executor",
			Prompt:       "Implement the dark mode feature",
		}

		runSpec, _ := spawner.buildRunSpec(spec, "session-456")

		if runSpec.PermissionMode != "default" {
			t.Errorf("Expected permission mode 'default', got '%s'", runSpec.PermissionMode)
		}

		// Executor should have all tools (nil means all)
		if runSpec.AllowedTools != nil {
			t.Errorf("Expected nil allowed tools for executor, got %v", runSpec.AllowedTools)
		}
	})

	t.Run("UnknownArchetype", func(t *testing.T) {
		spec := SpawnSpec{
			WorktreePath: "/home/user/repos/project",
			ProjectName:  "project",
			Archetype:    "unknown",
			Prompt:       "Do something",
		}

		runSpec, _ := spawner.buildRunSpec(spec, "session-789")

		// Should fall back to default model
		if runSpec.Model != "sonnet" {
			t.Errorf("Expected fallback model 'sonnet', got '%s'", runSpec.Model)
		}
	})
}

// TestSpawnerListRunning tests tracking of running processes
func TestSpawnerListRunning(t *testing.T) {
	st, cfg, cleanup := setupTestEnv(t)
	defer cleanup()

	spawner := NewSpawner(cfg, st, nil) // nil publisher for tests

	// Initially no running processes
	running := spawner.ListRunning()
	if len(running) != 0 {
		t.Errorf("Expected 0 running processes, got %d", len(running))
	}

	// Manually add a process to the map (simulating a spawn)
	spawner.mu.Lock()
	spawner.processes["agent-1"] = &ManagedProcess{
		AgentID:   "agent-1",
		SessionID: "session-1",
	}
	spawner.processes["agent-2"] = &ManagedProcess{
		AgentID:   "agent-2",
		SessionID: "session-2",
	}
	spawner.mu.Unlock()

	running = spawner.ListRunning()
	if len(running) != 2 {
		t.Errorf("Expected 2 running processes, got %d", len(running))
	}

	// GetProcess should find the process
	mp, ok := spawner.GetProcess("agent-1")
	if !ok {
		t.Error("Expected to find agent-1")
	}
	if mp.SessionID != "session-1" {
		t.Errorf("Expected session 'session-1', got '%s'", mp.SessionID)
	}

	// GetProcess for unknown agent should return false
	_, ok = spawner.GetProcess("unknown")
	if ok {
		t.Error("Did not expect to find unknown agent")
	}
}

// TestSpawnSpecValidation tests that spawn specs are properly validated
func TestSpawnSpecValidation(t *testing.T) {
	// This test documents expected fields in a SpawnSpec
	spec := SpawnSpec{
		WorktreePath: "/home/user/repos/project",
		ProjectName:  "project",
		Archetype:    "planner",
		Prompt:       "Analyze the codebase",
		ParentID:     "", // Optional for sub-agents
	}

	if spec.WorktreePath == "" {
		t.Error("WorktreePath should be set")
	}

	if spec.ProjectName == "" {
		t.Error("ProjectName should be set")
	}

	if spec.Archetype == "" {
		t.Error("Archetype should be set")
	}

	if spec.Prompt == "" {
		t.Error("Prompt should be set")
	}
}

// TestPipelineIntegration tests that the spawner creates a pipeline
func TestPipelineIntegration(t *testing.T) {
	st, cfg, cleanup := setupTestEnv(t)
	defer cleanup()

	spawner := NewSpawner(cfg, st, nil) // nil publisher for tests

	// Pipeline should be created
	pipeline := spawner.Pipeline()
	if pipeline == nil {
		t.Error("Expected pipeline to be created")
	}
}
