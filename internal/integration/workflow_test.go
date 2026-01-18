// Package integration provides end-to-end integration tests for Athena workflows.
package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drewfead/athena/internal/store"
	"github.com/google/uuid"
)

// TestNoteToFeatureWorkflow tests the full note-to-feature pipeline
// This is the core MVP workflow:
// 1. Create a note
// 2. Create a worktree for the feature
// 3. Create an agent for the worktree
// 4. Track agent status through lifecycle
// 5. Record to changelog on completion
func TestNoteToFeatureWorkflow(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	// Step 1: Capture an idea as a note (global, not project-scoped)
	noteID := uuid.NewString()
	note := &store.Note{
		ID:      noteID,
		Content: "Add dark mode toggle to settings panel",
		Done:    false,
	}

	if err := st.CreateNote(note); err != nil {
		t.Fatalf("Failed to create note: %v", err)
	}

	// Verify note was created
	notes, err := st.ListNotes()
	if err != nil {
		t.Fatalf("Failed to list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("Expected 1 note, got %d", len(notes))
	}
	t.Logf("Step 1: Note created - %s", note.Content)

	// Step 2: Promote note to feature - create a worktree
	worktreePath := "/home/user/repos/worktrees/athena-dark-mode-1234"
	wt := &store.Worktree{
		Path:        worktreePath,
		Project:     "athena",
		Branch:      "drew/dark-mode",
		IsMain:      false,
		Status:      store.WorktreeStatusActive,
		Description: strPtr(note.Content),
	}

	if err := st.UpsertWorktree(wt); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// Mark note as done after promotion
	if err := st.UpdateNoteDone(noteID, true); err != nil {
		t.Fatalf("Failed to mark note done: %v", err)
	}
	t.Logf("Step 2: Worktree created at %s", worktreePath)

	// Step 3: Spawn a planning agent for the worktree
	agentID := uuid.NewString()
	sessionID := uuid.NewString()
	agent := &store.Agent{
		ID:              agentID,
		WorktreePath:    worktreePath,
		ProjectName:     "athena",
		Archetype:       "planner",
		Status:          store.AgentStatusSpawning,
		Prompt:          "Analyze the codebase and create a plan for: " + note.Content,
		ClaudeSessionID: sessionID,
	}

	if err := st.CreateAgent(agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Link agent to worktree
	if err := st.AssignAgentToWorktree(worktreePath, agentID); err != nil {
		t.Fatalf("Failed to assign agent to worktree: %v", err)
	}
	t.Logf("Step 3: Planning agent spawned with ID %s", agentID)

	// Step 4: Simulate agent lifecycle - spawning -> running -> planning -> completed
	statusTransitions := []struct {
		status      store.AgentStatus
		description string
	}{
		{store.AgentStatusRunning, "Agent started"},
		{store.AgentStatusPlanning, "Agent exploring codebase"},
		{store.AgentStatusExecuting, "Agent using tools"},
		{store.AgentStatusCompleted, "Agent finished"},
	}

	for _, transition := range statusTransitions {
		if err := st.UpdateAgentStatus(agentID, transition.status); err != nil {
			t.Fatalf("Failed to update agent status to %s: %v", transition.status, err)
		}

		// Log an event for each transition
		if err := st.LogAgentEvent(agentID, string(transition.status), `{"status":"`+string(transition.status)+`"}`); err != nil {
			t.Fatalf("Failed to log agent event: %v", err)
		}
		t.Logf("Step 4: %s (status: %s)", transition.description, transition.status)
	}

	// Verify agent reached completed status
	completedAgent, err := st.GetAgent(agentID)
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if completedAgent.Status != store.AgentStatusCompleted {
		t.Errorf("Expected agent status 'completed', got '%s'", completedAgent.Status)
	}

	// Step 5: Record completion in changelog
	changelogID := uuid.NewString()
	changelog := &store.ChangelogEntry{
		ID:          changelogID,
		Title:       "Add dark mode toggle",
		Description: "Implemented dark mode toggle in settings panel per note",
		Category:    "feature",
		Project:     "athena",
		AgentID:     &agentID,
	}

	if err := st.CreateChangelogEntry(changelog); err != nil {
		t.Fatalf("Failed to create changelog entry: %v", err)
	}
	t.Logf("Step 5: Changelog entry created - %s", changelog.Title)

	// Verify the complete workflow
	t.Log("--- Workflow Verification ---")

	// Note should be done
	finalNote, _ := st.GetNote(noteID)
	if !finalNote.Done {
		t.Error("Note should be marked as done")
	}
	t.Log("Note: done")

	// Worktree should exist with agent assigned
	finalWt, _ := st.GetWorktree(worktreePath)
	if finalWt == nil {
		t.Fatal("Worktree should exist")
	}
	if finalWt.AgentID == nil || *finalWt.AgentID != agentID {
		t.Error("Worktree should have agent assigned")
	}
	t.Logf("Worktree: exists, agent=%s", agentID)

	// Agent should be completed
	t.Logf("Agent: status=%s, session=%s", completedAgent.Status, completedAgent.ClaudeSessionID)

	// Changelog should have entry
	entries, _ := st.ListChangelog("athena", 10)
	if len(entries) != 1 {
		t.Errorf("Expected 1 changelog entry, got %d", len(entries))
	}
	t.Logf("Changelog: %d entries", len(entries))
}

// TestAgentSessionResume tests the /resume workflow
// When a user attaches to an agent, they need the ClaudeSessionID
func TestAgentSessionResume(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	// Create an agent with a session ID
	sessionID := uuid.NewString()
	agent := &store.Agent{
		ID:              uuid.NewString(),
		WorktreePath:    "/home/user/repos/project-feature",
		ProjectName:     "project",
		Archetype:       "planner",
		Status:          store.AgentStatusRunning,
		ClaudeSessionID: sessionID,
	}

	if err := st.CreateAgent(agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Simulate: User wants to attach - lookup agent by session ID
	foundAgent, err := st.GetAgentBySessionID(sessionID)
	if err != nil {
		t.Fatalf("GetAgentBySessionID failed: %v", err)
	}

	if foundAgent == nil {
		t.Fatal("Expected to find agent by session ID")
	}

	if foundAgent.ID != agent.ID {
		t.Errorf("Found wrong agent: expected %s, got %s", agent.ID, foundAgent.ID)
	}

	// Verify session ID is usable for `claude --resume <session-id>`
	if foundAgent.ClaudeSessionID != sessionID {
		t.Errorf("Session ID mismatch: expected %s, got %s", sessionID, foundAgent.ClaudeSessionID)
	}

	t.Logf("Agent %s can be resumed with: claude --resume %s", agent.ID, sessionID)
}

// TestMultipleWorktreesPerProject tests managing multiple worktrees
func TestMultipleWorktreesPerProject(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	// Create main repo worktree
	main := &store.Worktree{
		Path:    "/home/user/repos/athena",
		Project: "athena",
		Branch:  "main",
		IsMain:  true,
	}
	if err := st.UpsertWorktree(main); err != nil {
		t.Fatalf("Failed to create main worktree: %v", err)
	}

	// Create feature worktrees
	features := []struct {
		path   string
		branch string
	}{
		{"/home/user/repos/worktrees/athena-dark-mode", "drew/dark-mode"},
		{"/home/user/repos/worktrees/athena-api-client", "drew/api-client"},
		{"/home/user/repos/worktrees/athena-tests", "drew/add-tests"},
	}

	for _, f := range features {
		wt := &store.Worktree{
			Path:    f.path,
			Project: "athena",
			Branch:  f.branch,
			IsMain:  false,
		}
		if err := st.UpsertWorktree(wt); err != nil {
			t.Fatalf("Failed to create worktree %s: %v", f.path, err)
		}
	}

	// List all worktrees for the project
	worktrees, err := st.ListWorktrees("athena")
	if err != nil {
		t.Fatalf("Failed to list worktrees: %v", err)
	}

	if len(worktrees) != 4 {
		t.Errorf("Expected 4 worktrees, got %d", len(worktrees))
	}

	// First should be main (sorted by is_main DESC)
	if !worktrees[0].IsMain {
		t.Error("Expected first worktree to be main")
	}

	t.Logf("Project 'athena' has %d worktrees:", len(worktrees))
	for _, wt := range worktrees {
		main := ""
		if wt.IsMain {
			main = " (main)"
		}
		t.Logf("  - %s: %s%s", wt.Branch, wt.Path, main)
	}
}

// TestAgentCrashRecovery tests handling of crashed agents
func TestAgentCrashRecovery(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	// Create an agent that will crash
	agentID := uuid.NewString()
	agent := &store.Agent{
		ID:           agentID,
		WorktreePath: "/home/user/repos/project-feature",
		ProjectName:  "project",
		Archetype:    "executor",
		Status:       store.AgentStatusRunning,
	}

	if err := st.CreateAgent(agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Simulate crash
	if err := st.UpdateAgentStatus(agentID, store.AgentStatusCrashed); err != nil {
		t.Fatalf("Failed to update status to crashed: %v", err)
	}
	if err := st.UpdateAgentExitCode(agentID, 1); err != nil {
		t.Fatalf("Failed to update exit code: %v", err)
	}

	// Verify crashed state
	crashed, _ := st.GetAgent(agentID)
	if crashed.Status != store.AgentStatusCrashed {
		t.Errorf("Expected status 'crashed', got '%s'", crashed.Status)
	}
	if crashed.ExitCode == nil || *crashed.ExitCode != 1 {
		t.Errorf("Expected exit code 1, got %v", crashed.ExitCode)
	}

	// Increment restart count (simulating auto-restart logic)
	if err := st.IncrementRestartCount(agentID); err != nil {
		t.Fatalf("Failed to increment restart count: %v", err)
	}

	// Restart: create new agent with incremented counter
	if err := st.UpdateAgentStatus(agentID, store.AgentStatusSpawning); err != nil {
		t.Fatalf("Failed to update status to spawning: %v", err)
	}

	// Verify restart count
	restarted, _ := st.GetAgent(agentID)
	if restarted.RestartCount != 1 {
		t.Errorf("Expected restart count 1, got %d", restarted.RestartCount)
	}

	t.Logf("Agent %s crashed and restarted (count: %d)", agentID, restarted.RestartCount)
}

// TestWorktreeStatusLifecycle tests worktree status transitions
func TestWorktreeStatusLifecycle(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	path := "/home/user/repos/worktrees/athena-feature"
	wt := &store.Worktree{
		Path:    path,
		Project: "athena",
		Branch:  "drew/feature",
		Status:  store.WorktreeStatusActive,
	}

	if err := st.UpsertWorktree(wt); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// Active -> Merged (branch was merged to main)
	if err := st.UpdateWorktreeStatus(path, store.WorktreeStatusMerged); err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	merged, _ := st.GetWorktree(path)
	if merged.Status != store.WorktreeStatusMerged {
		t.Errorf("Expected status 'merged', got '%s'", merged.Status)
	}

	// List by status
	mergedWorktrees, err := st.ListWorktreesByStatus(store.WorktreeStatusMerged)
	if err != nil {
		t.Fatalf("Failed to list by status: %v", err)
	}

	if len(mergedWorktrees) != 1 {
		t.Errorf("Expected 1 merged worktree, got %d", len(mergedWorktrees))
	}

	t.Logf("Worktree %s transitioned: active -> merged", path)
}

// Helper functions

func setupTestStore(t *testing.T) (*store.Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "athena-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create store: %v", err)
	}

	cleanup := func() {
		st.Close()
		os.RemoveAll(tmpDir)
	}

	return st, cleanup
}

func strPtr(s string) *string {
	return &s
}
