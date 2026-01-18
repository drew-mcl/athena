package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "athena-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := New(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}

	cleanup := func() {
		st.Close()
		os.RemoveAll(tmpDir)
	}

	return st, cleanup
}

// TestNotes tests the notes CRUD operations
func TestNotes(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	t.Run("CreateAndList", func(t *testing.T) {
		note := &Note{
			ID:      "note-1",
			Content: "Add dark mode toggle",
			Done:    false,
		}

		if err := st.CreateNote(note); err != nil {
			t.Fatalf("CreateNote failed: %v", err)
		}

		notes, err := st.ListNotes()
		if err != nil {
			t.Fatalf("ListNotes failed: %v", err)
		}

		if len(notes) != 1 {
			t.Fatalf("expected 1 note, got %d", len(notes))
		}

		if notes[0].Content != "Add dark mode toggle" {
			t.Errorf("expected content 'Add dark mode toggle', got '%s'", notes[0].Content)
		}
	})

	t.Run("GetNote", func(t *testing.T) {
		note, err := st.GetNote("note-1")
		if err != nil {
			t.Fatalf("GetNote failed: %v", err)
		}

		if note == nil {
			t.Fatal("expected note, got nil")
		}

		if note.ID != "note-1" {
			t.Errorf("expected ID 'note-1', got '%s'", note.ID)
		}
	})

	t.Run("UpdateNoteDone", func(t *testing.T) {
		if err := st.UpdateNoteDone("note-1", true); err != nil {
			t.Fatalf("UpdateNoteDone failed: %v", err)
		}

		note, err := st.GetNote("note-1")
		if err != nil {
			t.Fatalf("GetNote failed: %v", err)
		}

		if !note.Done {
			t.Error("expected note.Done to be true")
		}
	})

	t.Run("DeleteNote", func(t *testing.T) {
		if err := st.DeleteNote("note-1"); err != nil {
			t.Fatalf("DeleteNote failed: %v", err)
		}

		note, err := st.GetNote("note-1")
		if err != nil {
			t.Fatalf("GetNote failed: %v", err)
		}

		if note != nil {
			t.Error("expected note to be nil after deletion")
		}
	})
}

// TestWorktrees tests the worktree CRUD operations
func TestWorktrees(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	t.Run("UpsertAndList", func(t *testing.T) {
		wt := &Worktree{
			Path:    "/home/user/repos/project-feature",
			Project: "project",
			Branch:  "feature-branch",
			IsMain:  false,
			Status:  WorktreeStatusActive,
		}

		if err := st.UpsertWorktree(wt); err != nil {
			t.Fatalf("UpsertWorktree failed: %v", err)
		}

		worktrees, err := st.ListWorktrees("")
		if err != nil {
			t.Fatalf("ListWorktrees failed: %v", err)
		}

		if len(worktrees) != 1 {
			t.Fatalf("expected 1 worktree, got %d", len(worktrees))
		}

		if worktrees[0].Branch != "feature-branch" {
			t.Errorf("expected branch 'feature-branch', got '%s'", worktrees[0].Branch)
		}
	})

	t.Run("GetWorktree", func(t *testing.T) {
		wt, err := st.GetWorktree("/home/user/repos/project-feature")
		if err != nil {
			t.Fatalf("GetWorktree failed: %v", err)
		}

		if wt == nil {
			t.Fatal("expected worktree, got nil")
		}

		if wt.Project != "project" {
			t.Errorf("expected project 'project', got '%s'", wt.Project)
		}
	})

	t.Run("FilterByProject", func(t *testing.T) {
		// Add another worktree for a different project
		wt2 := &Worktree{
			Path:    "/home/user/repos/other-feature",
			Project: "other",
			Branch:  "main",
			IsMain:  true,
		}
		if err := st.UpsertWorktree(wt2); err != nil {
			t.Fatalf("UpsertWorktree failed: %v", err)
		}

		// Filter by project
		worktrees, err := st.ListWorktrees("project")
		if err != nil {
			t.Fatalf("ListWorktrees failed: %v", err)
		}

		if len(worktrees) != 1 {
			t.Fatalf("expected 1 worktree for 'project', got %d", len(worktrees))
		}
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		if err := st.UpdateWorktreeStatus("/home/user/repos/project-feature", WorktreeStatusMerged); err != nil {
			t.Fatalf("UpdateWorktreeStatus failed: %v", err)
		}

		wt, err := st.GetWorktree("/home/user/repos/project-feature")
		if err != nil {
			t.Fatalf("GetWorktree failed: %v", err)
		}

		if wt.Status != WorktreeStatusMerged {
			t.Errorf("expected status 'merged', got '%s'", wt.Status)
		}
	})
}

// TestAgents tests the agent CRUD operations
func TestAgents(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	t.Run("CreateAndList", func(t *testing.T) {
		agent := &Agent{
			ID:              "agent-1",
			WorktreePath:    "/home/user/repos/project-feature",
			ProjectName:     "project",
			Archetype:       "planner",
			Status:          AgentStatusSpawning,
			Prompt:          "Analyze the codebase and create a plan",
			ClaudeSessionID: "session-123",
		}

		if err := st.CreateAgent(agent); err != nil {
			t.Fatalf("CreateAgent failed: %v", err)
		}

		agents, err := st.ListAgents()
		if err != nil {
			t.Fatalf("ListAgents failed: %v", err)
		}

		if len(agents) != 1 {
			t.Fatalf("expected 1 agent, got %d", len(agents))
		}

		if agents[0].Archetype != "planner" {
			t.Errorf("expected archetype 'planner', got '%s'", agents[0].Archetype)
		}
	})

	t.Run("GetAgent", func(t *testing.T) {
		agent, err := st.GetAgent("agent-1")
		if err != nil {
			t.Fatalf("GetAgent failed: %v", err)
		}

		if agent == nil {
			t.Fatal("expected agent, got nil")
		}

		if agent.ClaudeSessionID != "session-123" {
			t.Errorf("expected session 'session-123', got '%s'", agent.ClaudeSessionID)
		}
	})

	t.Run("GetAgentBySessionID", func(t *testing.T) {
		agent, err := st.GetAgentBySessionID("session-123")
		if err != nil {
			t.Fatalf("GetAgentBySessionID failed: %v", err)
		}

		if agent == nil {
			t.Fatal("expected agent, got nil")
		}

		if agent.ID != "agent-1" {
			t.Errorf("expected ID 'agent-1', got '%s'", agent.ID)
		}
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		if err := st.UpdateAgentStatus("agent-1", AgentStatusRunning); err != nil {
			t.Fatalf("UpdateAgentStatus failed: %v", err)
		}

		agent, err := st.GetAgent("agent-1")
		if err != nil {
			t.Fatalf("GetAgent failed: %v", err)
		}

		if agent.Status != AgentStatusRunning {
			t.Errorf("expected status 'running', got '%s'", agent.Status)
		}
	})

	t.Run("UpdatePID", func(t *testing.T) {
		if err := st.UpdateAgentPID("agent-1", 12345); err != nil {
			t.Fatalf("UpdateAgentPID failed: %v", err)
		}

		agent, err := st.GetAgent("agent-1")
		if err != nil {
			t.Fatalf("GetAgent failed: %v", err)
		}

		if agent.PID == nil || *agent.PID != 12345 {
			t.Errorf("expected PID 12345, got %v", agent.PID)
		}
	})

	t.Run("ListByStatus", func(t *testing.T) {
		// Add a completed agent
		agent2 := &Agent{
			ID:           "agent-2",
			WorktreePath: "/home/user/repos/other",
			ProjectName:  "other",
			Archetype:    "executor",
			Status:       AgentStatusCompleted,
		}
		if err := st.CreateAgent(agent2); err != nil {
			t.Fatalf("CreateAgent failed: %v", err)
		}

		// List running agents only
		agents, err := st.ListAgents(AgentStatusRunning)
		if err != nil {
			t.Fatalf("ListAgents failed: %v", err)
		}

		if len(agents) != 1 {
			t.Fatalf("expected 1 running agent, got %d", len(agents))
		}

		if agents[0].ID != "agent-1" {
			t.Errorf("expected agent-1, got '%s'", agents[0].ID)
		}
	})

	t.Run("LogAndGetEvents", func(t *testing.T) {
		if err := st.LogAgentEvent("agent-1", "thinking", `{"content":"analyzing..."}`); err != nil {
			t.Fatalf("LogAgentEvent failed: %v", err)
		}

		if err := st.LogAgentEvent("agent-1", "tool_use", `{"tool":"read_file"}`); err != nil {
			t.Fatalf("LogAgentEvent failed: %v", err)
		}

		events, err := st.GetAgentEvents("agent-1", 10)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
	})
}

// TestWorktreeAgentRelationship tests linking agents to worktrees
func TestWorktreeAgentRelationship(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	// Create worktree
	wt := &Worktree{
		Path:    "/home/user/repos/project-feature",
		Project: "project",
		Branch:  "feature",
	}
	if err := st.UpsertWorktree(wt); err != nil {
		t.Fatalf("UpsertWorktree failed: %v", err)
	}

	// Create agent
	agent := &Agent{
		ID:           "agent-1",
		WorktreePath: wt.Path,
		ProjectName:  "project",
		Archetype:    "planner",
		Status:       AgentStatusRunning,
	}
	if err := st.CreateAgent(agent); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	// Link agent to worktree
	if err := st.AssignAgentToWorktree(wt.Path, agent.ID); err != nil {
		t.Fatalf("AssignAgentToWorktree failed: %v", err)
	}

	// Verify linkage
	wt, err := st.GetWorktree(wt.Path)
	if err != nil {
		t.Fatalf("GetWorktree failed: %v", err)
	}

	if wt.AgentID == nil || *wt.AgentID != "agent-1" {
		t.Errorf("expected agent_id 'agent-1', got %v", wt.AgentID)
	}

	// List worktrees with agents
	worktrees, err := st.ListWorktreesWithAgents()
	if err != nil {
		t.Fatalf("ListWorktreesWithAgents failed: %v", err)
	}

	if len(worktrees) != 1 {
		t.Fatalf("expected 1 worktree with agent, got %d", len(worktrees))
	}

	// Clear agent
	if err := st.ClearWorktreeAgent(wt.Path); err != nil {
		t.Fatalf("ClearWorktreeAgent failed: %v", err)
	}

	wt, _ = st.GetWorktree(wt.Path)
	if wt.AgentID != nil {
		t.Error("expected agent_id to be nil after clearing")
	}
}

// TestChangelog tests changelog operations
func TestChangelog(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	t.Run("CreateAndList", func(t *testing.T) {
		entry := &ChangelogEntry{
			ID:          "changelog-1",
			Title:       "Add dark mode",
			Description: "Implemented dark mode toggle in settings",
			Category:    "feature",
			Project:     "athena",
			CreatedAt:   time.Now(),
		}

		if err := st.CreateChangelogEntry(entry); err != nil {
			t.Fatalf("CreateChangelogEntry failed: %v", err)
		}

		entries, err := st.ListChangelog("athena", 10)
		if err != nil {
			t.Fatalf("ListChangelog failed: %v", err)
		}

		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		if entries[0].Category != "feature" {
			t.Errorf("expected category 'feature', got '%s'", entries[0].Category)
		}
	})
}
