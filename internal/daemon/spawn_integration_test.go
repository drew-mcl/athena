package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/store"
)

// newTestStore creates a real SQLite store backed by a temp file.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// newTestDaemon creates a minimal Daemon with a real store and config,
// suitable for testing spawn logic that doesn't need git or a real spawner.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	s := newTestStore(t)
	cfg := config.DefaultConfig()

	// Use a temp dir for worktree base so path computations don't fail
	cfg.Repos.WorktreeDir = t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return &Daemon{
		store:  s,
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

func mustMarshalSpawnRequest(t *testing.T, req control.SpawnRequest) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal spawn request: %v", err)
	}
	return encoded
}

// --- resolveSpawnTarget tests ---

func TestResolveSpawnTarget_BareMode(t *testing.T) {
	d := newTestDaemon(t)

	wi, parentGoal, ticketCtx, err := d.resolveSpawnTarget(control.SpawnRequest{}, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wi == nil {
		t.Fatal("expected work item, got nil")
	}
	if wi.ItemType != store.WorkItemTypeTask {
		t.Errorf("expected task type, got %s", wi.ItemType)
	}
	if wi.Project != "myproject" {
		t.Errorf("expected project 'myproject', got %s", wi.Project)
	}
	if wi.Status != store.WorkItemStatusInProgress {
		t.Errorf("expected in_progress status, got %s", wi.Status)
	}
	if wi.Subject != "Interactive session" {
		t.Errorf("expected subject 'Interactive session', got %s", wi.Subject)
	}
	if parentGoal != nil {
		t.Errorf("expected nil parent goal for bare spawn, got %+v", parentGoal)
	}
	if ticketCtx != "" {
		t.Errorf("expected empty ticket context for bare spawn, got %q", ticketCtx)
	}

	// Verify the work item was persisted
	stored, err := d.store.GetWorkItem(wi.ID)
	if err != nil {
		t.Fatalf("failed to get work item from store: %v", err)
	}
	if stored == nil {
		t.Fatal("work item not found in store after creation")
	}
	if stored.ID != wi.ID {
		t.Errorf("stored ID mismatch: got %s, want %s", stored.ID, wi.ID)
	}
}

func TestResolveSpawnTarget_WorkItemMode(t *testing.T) {
	d := newTestDaemon(t)

	// Create a pre-existing work item
	wi := &store.WorkItem{
		ID:          "wi-test-1",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Build the thing",
		Description: "A detailed description",
		Status:      store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(wi); err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	req := control.SpawnRequest{WorkItemID: "wi-test-1"}
	got, parentGoal, ticketCtx, err := d.resolveSpawnTarget(req, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "wi-test-1" {
		t.Errorf("expected ID 'wi-test-1', got %s", got.ID)
	}
	if got.Subject != "Build the thing" {
		t.Errorf("expected subject 'Build the thing', got %s", got.Subject)
	}
	if parentGoal != nil {
		t.Errorf("expected nil parent goal, got %+v", parentGoal)
	}
	if ticketCtx != "" {
		t.Errorf("expected empty ticket context, got %q", ticketCtx)
	}
}

func TestResolveSpawnTarget_WorkItemNotFound(t *testing.T) {
	d := newTestDaemon(t)

	req := control.SpawnRequest{WorkItemID: "wi-nonexistent"}
	_, _, _, err := d.resolveSpawnTarget(req, "myproject")
	if err == nil {
		t.Fatal("expected error for nonexistent work item, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestResolveSpawnTarget_FeatureNotFound(t *testing.T) {
	d := newTestDaemon(t)

	req := control.SpawnRequest{FeatureID: "wi-nonexistent"}
	_, _, _, err := d.resolveSpawnTarget(req, "myproject")
	if err == nil {
		t.Fatal("expected error for nonexistent feature, got nil")
	}
	if !strings.Contains(err.Error(), "feature not found") {
		t.Errorf("expected 'feature not found' in error, got: %v", err)
	}
}

func TestResolveSpawnTarget_FeatureWrongType(t *testing.T) {
	d := newTestDaemon(t)

	// Create a goal (not a feature)
	goal := &store.WorkItem{
		ID:       "wi-goal",
		Project:  "myproject",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "A goal, not a feature",
		Status:   store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatalf("failed to create goal: %v", err)
	}

	req := control.SpawnRequest{FeatureID: "wi-goal"}
	_, _, _, err := d.resolveSpawnTarget(req, "myproject")
	if err == nil {
		t.Fatal("expected error when using goal ID as feature, got nil")
	}
	if !strings.Contains(err.Error(), "not a feature") {
		t.Errorf("expected 'not a feature' in error, got: %v", err)
	}
}

func TestResolveSpawnTarget_TicketMode(t *testing.T) {
	d := newTestDaemon(t)

	// Ticket mode with no PM plugins will still create a goal work item
	req := control.SpawnRequest{TicketID: "ENG-999"}
	wi, parentGoal, ticketCtx, err := d.resolveSpawnTarget(req, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wi == nil {
		t.Fatal("expected work item, got nil")
	}
	if wi.ItemType != store.WorkItemTypeGoal {
		t.Errorf("expected goal type, got %s", wi.ItemType)
	}
	if wi.TicketID == nil || *wi.TicketID != "ENG-999" {
		t.Errorf("expected ticket ID 'ENG-999', got %v", wi.TicketID)
	}
	if parentGoal != nil {
		t.Errorf("expected nil parent goal for ticket spawn, got %+v", parentGoal)
	}
	if !strings.Contains(ticketCtx, "ENG-999") {
		t.Errorf("expected ticket context to contain 'ENG-999', got %q", ticketCtx)
	}

	// Verify persistence
	stored, err := d.store.GetWorkItem(wi.ID)
	if err != nil || stored == nil {
		t.Fatalf("work item not persisted: err=%v", err)
	}
}

func TestResolveSpawnTarget_TicketExistingWorkItem(t *testing.T) {
	d := newTestDaemon(t)

	// Pre-create a work item with the ticket
	ticketID := "ENG-123"
	existing := &store.WorkItem{
		ID:          "wi-existing",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Existing ticket item",
		Description: "Already tracked",
		Status:      store.WorkItemStatusPending,
		TicketID:    &ticketID,
	}
	if err := d.store.CreateWorkItem(existing); err != nil {
		t.Fatalf("failed to create existing work item: %v", err)
	}

	req := control.SpawnRequest{TicketID: "ENG-123"}
	wi, _, ticketCtx, err := d.resolveSpawnTarget(req, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the existing item, not create a new one
	if wi.ID != "wi-existing" {
		t.Errorf("expected to reuse existing work item 'wi-existing', got %s", wi.ID)
	}
	if !strings.Contains(ticketCtx, "Existing ticket item") {
		t.Errorf("expected ticket context to include existing subject, got %q", ticketCtx)
	}
}

// --- createBareWorkItem tests ---

func TestCreateBareWorkItem(t *testing.T) {
	d := newTestDaemon(t)

	wi, err := d.createBareWorkItem("testproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wi.Project != "testproject" {
		t.Errorf("expected project 'testproject', got %s", wi.Project)
	}
	if wi.ItemType != store.WorkItemTypeTask {
		t.Errorf("expected task type, got %s", wi.ItemType)
	}
	if wi.Subject != "Interactive session" {
		t.Errorf("expected subject 'Interactive session', got %s", wi.Subject)
	}
	if !strings.HasPrefix(wi.ID, "wi-") {
		t.Errorf("expected ID to start with 'wi-', got %s", wi.ID)
	}

	// Create a second one and verify unique IDs
	wi2, err := d.createBareWorkItem("testproject")
	if err != nil {
		t.Fatalf("unexpected error creating second work item: %v", err)
	}
	if wi2.ID == wi.ID {
		t.Errorf("expected unique IDs, both got %s", wi.ID)
	}
}

// --- resolveFeatureSpawn tests ---

func TestResolveFeatureSpawn_WithParentGoal(t *testing.T) {
	d := newTestDaemon(t)

	// Create a goal
	goal := &store.WorkItem{
		ID:          "wi-goal",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Parent goal",
		Description: "The overarching goal",
		Status:      store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatalf("failed to create goal: %v", err)
	}

	// Register a worktree in the store first (FK constraint)
	wtPath := "/tmp/test-worktree"
	wt := &store.Worktree{
		Path:    wtPath,
		Project: "myproject",
		Branch:  "feat/wi-goal.1",
		IsMain:  false,
		Status:  store.WorktreeStatusActive,
	}
	if err := d.store.UpsertWorktree(wt); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Create a feature under the goal, with a pre-existing worktree path
	// (so we skip the CreateWorktree call which needs git)
	parentID := "wi-goal"
	feature := &store.WorkItem{
		ID:           "wi-goal.1",
		Project:      "myproject",
		ItemType:     store.WorkItemTypeFeature,
		ParentID:     &parentID,
		Subject:      "Feature under goal",
		Description:  "A feature",
		Status:       store.WorkItemStatusPending,
		WorktreePath: &wtPath,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatalf("failed to create feature: %v", err)
	}

	wi, parentGoal, ticketCtx, err := d.resolveFeatureSpawn("wi-goal.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the feature was returned
	if wi.ID != "wi-goal.1" {
		t.Errorf("expected feature ID 'wi-goal.1', got %s", wi.ID)
	}

	// Check status was updated to in_progress
	if wi.Status != store.WorkItemStatusInProgress {
		t.Errorf("expected in_progress status, got %s", wi.Status)
	}

	// Check parent goal was found
	if parentGoal == nil {
		t.Fatal("expected parent goal, got nil")
	}
	if parentGoal.ID != "wi-goal" {
		t.Errorf("expected parent ID 'wi-goal', got %s", parentGoal.ID)
	}
	if parentGoal.Subject != "Parent goal" {
		t.Errorf("expected parent subject 'Parent goal', got %s", parentGoal.Subject)
	}

	// No ticket context for feature spawn
	if ticketCtx != "" {
		t.Errorf("expected empty ticket context, got %q", ticketCtx)
	}

	// Verify status was persisted
	stored, err := d.store.GetWorkItem("wi-goal.1")
	if err != nil || stored == nil {
		t.Fatalf("failed to get work item: err=%v", err)
	}
	if stored.Status != store.WorkItemStatusInProgress {
		t.Errorf("expected persisted status in_progress, got %s", stored.Status)
	}
}

func TestResolveFeatureSpawn_NoParentGoal(t *testing.T) {
	d := newTestDaemon(t)

	// Register a worktree in the store first (FK constraint)
	wtPath := "/tmp/test-worktree-orphan"
	wt := &store.Worktree{
		Path:    wtPath,
		Project: "myproject",
		Branch:  "feat/wi-orphan",
		IsMain:  false,
		Status:  store.WorktreeStatusActive,
	}
	if err := d.store.UpsertWorktree(wt); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Create a feature with no parent
	feature := &store.WorkItem{
		ID:           "wi-orphan",
		Project:      "myproject",
		ItemType:     store.WorkItemTypeFeature,
		Subject:      "Orphan feature",
		Status:       store.WorkItemStatusPending,
		WorktreePath: &wtPath,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatalf("failed to create feature: %v", err)
	}

	wi, parentGoal, _, err := d.resolveFeatureSpawn("wi-orphan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wi.ID != "wi-orphan" {
		t.Errorf("expected feature ID 'wi-orphan', got %s", wi.ID)
	}
	if parentGoal != nil {
		t.Errorf("expected nil parent goal for orphan feature, got %+v", parentGoal)
	}
}

func TestResolveFeatureSpawn_NeedsWorktree(t *testing.T) {
	d := newTestDaemon(t)

	// Create a feature with no worktree path.
	// This will fail because we don't have a git repo or migrator,
	// but it tests the path that calls createFeatureWorktree.
	feature := &store.WorkItem{
		ID:       "wi-no-wt",
		Project:  "myproject",
		ItemType: store.WorkItemTypeFeature,
		Subject:  "Feature needs worktree",
		Status:   store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatalf("failed to create feature: %v", err)
	}

	_, _, _, err := d.resolveFeatureSpawn("wi-no-wt")
	if err == nil {
		t.Fatal("expected error when no migrator is available, got nil")
	}
	// The error should be about worktree creation failing (no migrator or no main repo)
	if !strings.Contains(err.Error(), "worktree") && !strings.Contains(err.Error(), "repo") {
		t.Errorf("expected worktree-related error, got: %v", err)
	}
}

// --- handleSpawn integration tests ---

func TestHandleSpawn_BareMode(t *testing.T) {
	d := newTestDaemon(t)

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		WorkDir: "/tmp/some-dir",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, ok := result.(*control.SpawnResponse)
	if !ok {
		t.Fatalf("expected *control.SpawnResponse, got %T", result)
	}

	// Bare mode creates an anonymous task
	if resp.WorkItem == nil {
		t.Fatal("expected WorkItem in response")
	}
	if resp.WorkItem.ItemType != "task" {
		t.Errorf("expected task type, got %s", resp.WorkItem.ItemType)
	}
	if resp.WorkItem.Status != "in_progress" {
		t.Errorf("expected in_progress status, got %s", resp.WorkItem.Status)
	}

	// TaskListID should be the work item ID
	if resp.TaskListID == "" {
		t.Error("expected non-empty TaskListID")
	}
	if resp.TaskListID != resp.WorkItem.ID {
		t.Errorf("TaskListID (%s) should match work item ID (%s)", resp.TaskListID, resp.WorkItem.ID)
	}

	// Interactive mode (not headless) should have ExecArgs
	if len(resp.ExecArgs) == 0 {
		t.Error("expected ExecArgs for interactive spawn")
	}
	if resp.ExecArgs[0] != "claude" {
		t.Errorf("expected first exec arg 'claude', got %s", resp.ExecArgs[0])
	}

	// Should NOT have agent info (that's headless only)
	if resp.Agent != nil {
		t.Errorf("expected nil agent for interactive mode, got %+v", resp.Agent)
	}

	// Check ExecEnv has task list ID
	foundTaskEnv := false
	for _, env := range resp.ExecEnv {
		if strings.HasPrefix(env, "CLAUDE_CODE_TASK_LIST_ID=") {
			foundTaskEnv = true
			break
		}
	}
	if !foundTaskEnv {
		t.Error("expected CLAUDE_CODE_TASK_LIST_ID in ExecEnv")
	}
}

func TestHandleSpawn_BareMode_DefaultProject(t *testing.T) {
	d := newTestDaemon(t)

	// No project specified - should default to "default"
	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)
	if resp.WorkItem.Project != "default" {
		t.Errorf("expected project 'default', got %s", resp.WorkItem.Project)
	}
}

func TestHandleSpawn_WorkItemMode(t *testing.T) {
	d := newTestDaemon(t)

	// Create a work item first
	wi := &store.WorkItem{
		ID:          "wi-handle-test",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Handle spawn test",
		Description: "Testing the full handler flow",
		Status:      store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(wi); err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		WorkItemID: "wi-handle-test",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)
	if resp.WorkItem == nil {
		t.Fatal("expected WorkItem in response")
	}
	if resp.WorkItem.ID != "wi-handle-test" {
		t.Errorf("expected ID 'wi-handle-test', got %s", resp.WorkItem.ID)
	}
	if resp.WorkItem.Subject != "Handle spawn test" {
		t.Errorf("expected subject 'Handle spawn test', got %s", resp.WorkItem.Subject)
	}

	// Should have exec args (interactive mode)
	if len(resp.ExecArgs) == 0 {
		t.Error("expected ExecArgs for interactive spawn")
	}
}

func TestHandleSpawn_FeatureNotFound(t *testing.T) {
	d := newTestDaemon(t)

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		FeatureID: "wi-nonexistent",
	})

	_, err := d.handleSpawn(reqJSON)
	if err == nil {
		t.Fatal("expected error for nonexistent feature")
	}
	if !strings.Contains(err.Error(), "feature not found") {
		t.Errorf("expected 'feature not found' in error, got: %v", err)
	}
}

func TestHandleSpawn_FeatureWrongType(t *testing.T) {
	d := newTestDaemon(t)

	goal := &store.WorkItem{
		ID:       "wi-wrong-type",
		Project:  "myproject",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "This is a goal",
		Status:   store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatalf("failed to create goal: %v", err)
	}

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		FeatureID: "wi-wrong-type",
	})

	_, err := d.handleSpawn(reqJSON)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "not a feature") {
		t.Errorf("expected 'not a feature' in error, got: %v", err)
	}
}

func TestHandleSpawn_TicketMode(t *testing.T) {
	d := newTestDaemon(t)

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		TicketID: "ENG-456",
		Project:  "myproject",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)
	if resp.WorkItem == nil {
		t.Fatal("expected WorkItem in response")
	}
	if resp.WorkItem.ItemType != "goal" {
		t.Errorf("expected goal type, got %s", resp.WorkItem.ItemType)
	}

	// TaskListID should be the work item ID
	if resp.TaskListID != resp.WorkItem.ID {
		t.Errorf("TaskListID mismatch: %s != %s", resp.TaskListID, resp.WorkItem.ID)
	}
}

func TestHandleSpawn_HeadlessNoWorkDir(t *testing.T) {
	d := newTestDaemon(t)

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		Headless: true,
		// No WorkDir and no WorktreePath on work item
	})

	_, err := d.handleSpawn(reqJSON)
	if err == nil {
		t.Fatal("expected error for headless spawn with no work dir")
	}
	if !strings.Contains(err.Error(), "no work directory") {
		t.Errorf("expected 'no work directory' in error, got: %v", err)
	}
}

func TestHandleSpawn_RetrieveMode(t *testing.T) {
	d := newTestDaemon(t)

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		Retrieve: true,
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)

	// Retrieve mode should use planner archetype
	found := false
	for _, arg := range resp.ExecArgs {
		if arg == "plan" {
			found = true
			break
		}
	}
	// The planner archetype has permission_mode "plan" in default config
	if !found {
		t.Logf("ExecArgs: %v", resp.ExecArgs)
		// Check if the --append-system-prompt contains retrieve mode info
		promptIdx := -1
		for i, arg := range resp.ExecArgs {
			if arg == "--append-system-prompt" && i+1 < len(resp.ExecArgs) {
				promptIdx = i + 1
				break
			}
		}
		if promptIdx == -1 {
			t.Error("expected --append-system-prompt in ExecArgs")
		} else {
			prompt := resp.ExecArgs[promptIdx]
			if !strings.Contains(prompt, "Retrieve & Plan") {
				t.Error("expected 'Retrieve & Plan' in prompt for retrieve mode")
			}
		}
	}
}

// --- buildSpawnPrompt integration tests (with real store data) ---

func TestBuildSpawnPrompt_WithParentGoal(t *testing.T) {
	d := newTestDaemon(t)

	goal := &store.WorkItem{
		ID:          "wi-prompt-goal",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Big strategic goal",
		Description: "Long-term objective for the team",
		Status:      store.WorkItemStatusPending,
	}
	parentID := "wi-prompt-goal"
	wtPath := "/tmp/test-wt"
	feature := &store.WorkItem{
		ID:           "wi-prompt-goal.1",
		Project:      "myproject",
		ItemType:     store.WorkItemTypeFeature,
		ParentID:     &parentID,
		Subject:      "Feature to build",
		Description:  "Specific implementation task",
		Status:       store.WorkItemStatusInProgress,
		WorktreePath: &wtPath,
	}

	prompt := d.buildSpawnPrompt(feature, goal, "", feature.ID, false)

	// Check goal context
	if !strings.Contains(prompt, "Big strategic goal") {
		t.Error("expected goal subject in prompt")
	}
	if !strings.Contains(prompt, "Long-term objective for the team") {
		t.Error("expected goal description in prompt")
	}

	// Check feature context
	if !strings.Contains(prompt, "Feature to build") {
		t.Error("expected feature subject in prompt")
	}
	if !strings.Contains(prompt, "Specific implementation task") {
		t.Error("expected feature description in prompt")
	}
	if !strings.Contains(prompt, "/tmp/test-wt") {
		t.Error("expected worktree path in prompt")
	}

	// Check task tracking section
	if !strings.Contains(prompt, feature.ID) {
		t.Error("expected task list ID in prompt")
	}

	// Check project is set
	if !strings.Contains(prompt, "myproject") {
		t.Error("expected project name in prompt")
	}
}

func TestBuildSpawnPrompt_RetrieveMode(t *testing.T) {
	d := newTestDaemon(t)

	wi := &store.WorkItem{
		ID:       "wi-retrieve",
		Project:  "myproject",
		ItemType: store.WorkItemTypeTask,
		Subject:  "Interactive session",
		Status:   store.WorkItemStatusInProgress,
	}

	prompt := d.buildSpawnPrompt(wi, nil, "", wi.ID, true)

	if !strings.Contains(prompt, "Retrieve & Plan") {
		t.Error("expected 'Retrieve & Plan' section in retrieve mode prompt")
	}
	if !strings.Contains(prompt, "Explore the codebase") {
		t.Error("expected exploration instructions in retrieve mode")
	}
}

func TestBuildSpawnPrompt_WithTicketContext(t *testing.T) {
	d := newTestDaemon(t)

	wi := &store.WorkItem{
		ID:       "wi-ticket",
		Project:  "myproject",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Fix the bug",
		Status:   store.WorkItemStatusInProgress,
	}
	ticketCtx := "## Ticket: ENG-123\n**Title:** Fix the bug\n**Description:** Detailed info here\n"

	prompt := d.buildSpawnPrompt(wi, nil, ticketCtx, wi.ID, false)

	if !strings.Contains(prompt, "ENG-123") {
		t.Error("expected ticket ID in prompt")
	}
	if !strings.Contains(prompt, "Detailed info here") {
		t.Error("expected ticket description in prompt")
	}
}

func TestBuildSpawnPrompt_NonFeatureWorkItem(t *testing.T) {
	d := newTestDaemon(t)

	wi := &store.WorkItem{
		ID:          "wi-task",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeTask,
		Subject:     "Do something specific",
		Description: "With these details",
		Status:      store.WorkItemStatusInProgress,
	}

	prompt := d.buildSpawnPrompt(wi, nil, "", wi.ID, false)

	// Non-feature items use a different format
	if !strings.Contains(prompt, "Do something specific") {
		t.Error("expected subject in prompt")
	}
	if !strings.Contains(prompt, "With these details") {
		t.Error("expected description in prompt")
	}
	// Should NOT contain "Feature" section header
	if strings.Contains(prompt, "## Feature") {
		t.Error("non-feature work items should not have ## Feature section")
	}
}

// --- buildInteractiveExec tests ---

func TestBuildInteractiveExec_ExecutorArchetype(t *testing.T) {
	d := newTestDaemon(t)

	args, env := d.buildInteractiveExec("test prompt", "executor", "wi-test")

	// Should start with 'claude'
	if len(args) == 0 || args[0] != "claude" {
		t.Fatalf("expected first arg 'claude', got %v", args)
	}

	// Should have --model from executor archetype (sonnet in default config)
	foundModel := false
	for i, arg := range args {
		if arg == "--model" && i+1 < len(args) {
			foundModel = true
			if args[i+1] != "sonnet" {
				t.Errorf("expected model 'sonnet' for executor, got %s", args[i+1])
			}
			break
		}
	}
	if !foundModel {
		t.Error("expected --model in exec args")
	}

	// Should have --append-system-prompt
	foundPrompt := false
	for i, arg := range args {
		if arg == "--append-system-prompt" && i+1 < len(args) {
			foundPrompt = true
			if args[i+1] != "test prompt" {
				t.Errorf("expected prompt 'test prompt', got %s", args[i+1])
			}
			break
		}
	}
	if !foundPrompt {
		t.Error("expected --append-system-prompt in exec args")
	}

	// Should have task list ID in env
	if len(env) == 0 {
		t.Fatal("expected env vars")
	}
	if env[0] != "CLAUDE_CODE_TASK_LIST_ID=wi-test" {
		t.Errorf("expected CLAUDE_CODE_TASK_LIST_ID=wi-test, got %s", env[0])
	}
}

func TestBuildInteractiveExec_PlannerArchetype(t *testing.T) {
	d := newTestDaemon(t)

	args, _ := d.buildInteractiveExec("test", "planner", "wi-test")

	// Planner should have --permission-mode plan and --model opus
	foundModel := false
	foundPermission := false
	for i, arg := range args {
		if arg == "--model" && i+1 < len(args) && args[i+1] == "opus" {
			foundModel = true
		}
		if arg == "--permission-mode" && i+1 < len(args) && args[i+1] == "plan" {
			foundPermission = true
		}
	}
	if !foundModel {
		t.Error("expected --model opus for planner archetype")
	}
	if !foundPermission {
		t.Error("expected --permission-mode plan for planner archetype")
	}
}

func TestBuildInteractiveExec_UnknownArchetype(t *testing.T) {
	d := newTestDaemon(t)

	args, _ := d.buildInteractiveExec("test", "nonexistent", "wi-test")

	// Should still have 'claude' and --append-system-prompt, just no archetype-specific flags
	if len(args) == 0 || args[0] != "claude" {
		t.Fatalf("expected first arg 'claude', got %v", args)
	}
	// Should have: claude --dangerously-skip-permissions --append-system-prompt <prompt>
	if len(args) != 4 {
		t.Errorf("expected 4 args for unknown archetype (claude + --dangerously-skip-permissions + --append-system-prompt + prompt), got %d: %v", len(args), args)
	}

	// Should have --dangerously-skip-permissions (default behavior)
	foundSkip := false
	for _, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			foundSkip = true
			break
		}
	}
	if !foundSkip {
		t.Error("expected --dangerously-skip-permissions in exec args (default on)")
	}
}

// --- resolveWorkItemSpawn tests ---

func TestResolveWorkItemSpawn_Success(t *testing.T) {
	d := newTestDaemon(t)

	wi := &store.WorkItem{
		ID:          "wi-resolve",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Resolvable item",
		Description: "Has all the context",
		Status:      store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(wi); err != nil {
		t.Fatal(err)
	}

	got, parent, ctx, err := d.resolveWorkItemSpawn("wi-resolve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "wi-resolve" {
		t.Errorf("expected ID 'wi-resolve', got %s", got.ID)
	}
	if parent != nil {
		t.Error("expected nil parent for work item spawn")
	}
	if ctx != "" {
		t.Error("expected empty ticket context for work item spawn")
	}
}

func TestResolveWorkItemSpawn_NotFound(t *testing.T) {
	d := newTestDaemon(t)

	_, _, _, err := d.resolveWorkItemSpawn("wi-missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// --- End-to-end test: bare spawn creates work item, builds prompt, returns exec args ---

func TestHandleSpawn_EndToEnd_BareInteractive(t *testing.T) {
	d := newTestDaemon(t)

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		Project: "e2e-project",
		WorkDir: t.TempDir(),
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)

	// 1. Work item was created and returned
	if resp.WorkItem == nil {
		t.Fatal("expected work item")
	}
	if resp.WorkItem.Project != "e2e-project" {
		t.Errorf("expected project 'e2e-project', got %s", resp.WorkItem.Project)
	}

	// 2. TaskListID matches work item ID
	if resp.TaskListID != resp.WorkItem.ID {
		t.Errorf("TaskListID (%s) != WorkItem.ID (%s)", resp.TaskListID, resp.WorkItem.ID)
	}

	// 3. ExecArgs are populated for interactive mode
	if len(resp.ExecArgs) == 0 {
		t.Fatal("expected ExecArgs")
	}

	// 4. Prompt is embedded in ExecArgs via --append-system-prompt
	promptIdx := -1
	for i, arg := range resp.ExecArgs {
		if arg == "--append-system-prompt" && i+1 < len(resp.ExecArgs) {
			promptIdx = i + 1
			break
		}
	}
	if promptIdx == -1 {
		t.Fatal("expected --append-system-prompt in ExecArgs")
	}
	prompt := resp.ExecArgs[promptIdx]
	if !strings.Contains(prompt, "Athena Context") {
		t.Error("expected 'Athena Context' header in prompt")
	}
	if !strings.Contains(prompt, "e2e-project") {
		t.Error("expected project name in prompt")
	}
	if !strings.Contains(prompt, resp.TaskListID) {
		t.Error("expected task list ID in prompt")
	}

	// 5. ExecEnv has CLAUDE_CODE_TASK_LIST_ID
	if len(resp.ExecEnv) == 0 {
		t.Fatal("expected ExecEnv")
	}
	expectedEnv := "CLAUDE_CODE_TASK_LIST_ID=" + resp.TaskListID
	if resp.ExecEnv[0] != expectedEnv {
		t.Errorf("expected env %q, got %q", expectedEnv, resp.ExecEnv[0])
	}

	// 6. No agent (interactive mode)
	if resp.Agent != nil {
		t.Error("expected nil agent for interactive mode")
	}

	// 7. Work item persisted in store
	stored, err := d.store.GetWorkItem(resp.WorkItem.ID)
	if err != nil || stored == nil {
		t.Fatal("work item not found in store")
	}
	if string(stored.Status) != resp.WorkItem.Status {
		t.Errorf("persisted status mismatch: %s vs %s", stored.Status, resp.WorkItem.Status)
	}
}

func TestHandleSpawn_EndToEnd_FeatureWithWorktree(t *testing.T) {
	d := newTestDaemon(t)

	// Set up a goal and feature with a pre-existing worktree path
	goal := &store.WorkItem{
		ID:          "wi-e2e-goal",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "E2E Goal",
		Description: "Full end-to-end goal",
		Status:      store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	// Create a temp dir to serve as the worktree path
	wtDir := t.TempDir()

	// Register the worktree in the store FIRST (FK constraint on work_items.worktree_path)
	wt := &store.Worktree{
		Path:    wtDir,
		Project: "myproject",
		Branch:  "feat/wi-e2e-goal.1",
		IsMain:  false,
		Status:  store.WorktreeStatusActive,
	}
	if err := d.store.UpsertWorktree(wt); err != nil {
		t.Fatal(err)
	}

	parentID := "wi-e2e-goal"
	feature := &store.WorkItem{
		ID:           "wi-e2e-goal.1",
		Project:      "myproject",
		ItemType:     store.WorkItemTypeFeature,
		ParentID:     &parentID,
		Subject:      "E2E Feature",
		Description:  "Feature implementation",
		Status:       store.WorkItemStatusPending,
		WorktreePath: &wtDir,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatal(err)
	}

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		FeatureID: "wi-e2e-goal.1",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)

	// Work item returned
	if resp.WorkItem.ID != "wi-e2e-goal.1" {
		t.Errorf("expected feature ID, got %s", resp.WorkItem.ID)
	}

	// TaskListID should be the feature ID
	if resp.TaskListID != "wi-e2e-goal.1" {
		t.Errorf("expected TaskListID 'wi-e2e-goal.1', got %s", resp.TaskListID)
	}

	// Worktree info should be populated
	if resp.Worktree == nil {
		t.Fatal("expected worktree info in response")
	}
	if resp.Worktree.Path != wtDir {
		t.Errorf("expected worktree path %s, got %s", wtDir, resp.Worktree.Path)
	}
	if resp.Worktree.Branch != "feat/wi-e2e-goal.1" {
		t.Errorf("expected branch 'feat/wi-e2e-goal.1', got %s", resp.Worktree.Branch)
	}

	// Prompt should contain goal context
	promptIdx := -1
	for i, arg := range resp.ExecArgs {
		if arg == "--append-system-prompt" && i+1 < len(resp.ExecArgs) {
			promptIdx = i + 1
			break
		}
	}
	if promptIdx >= 0 {
		prompt := resp.ExecArgs[promptIdx]
		if !strings.Contains(prompt, "E2E Goal") {
			t.Error("expected goal subject in prompt")
		}
		if !strings.Contains(prompt, "E2E Feature") {
			t.Error("expected feature subject in prompt")
		}
		if !strings.Contains(prompt, "Full end-to-end goal") {
			t.Error("expected goal description in prompt")
		}
	}

	// Feature should be marked in_progress
	stored, err := d.store.GetWorkItem("wi-e2e-goal.1")
	if err != nil || stored == nil {
		t.Fatal("feature not found in store")
	}
	if stored.Status != store.WorkItemStatusInProgress {
		t.Errorf("expected in_progress status, got %s", stored.Status)
	}
}

func TestHandleSpawn_InvalidJSON(t *testing.T) {
	d := newTestDaemon(t)

	_, err := d.handleSpawn(json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- Verify work item ID format ---

func TestWorkItemIDFormat(t *testing.T) {
	d := newTestDaemon(t)

	wi, err := d.createBareWorkItem("test")
	if err != nil {
		t.Fatal(err)
	}

	// IDs should start with "wi-" prefix
	if !strings.HasPrefix(wi.ID, "wi-") {
		t.Errorf("expected ID starting with 'wi-', got %s", wi.ID)
	}

	// ID should be a reasonable length (wi- + 4 hex chars = 7)
	if len(wi.ID) < 5 {
		t.Errorf("expected ID length >= 5, got %d for %s", len(wi.ID), wi.ID)
	}
}

// --- Test findMainRepoPath ---

func TestFindMainRepoPath_Found(t *testing.T) {
	d := newTestDaemon(t)

	// Create a main repo worktree entry
	mainPath := filepath.Join(t.TempDir(), "test-main-repo")
	wt := &store.Worktree{
		Path:    mainPath,
		Project: "myproject",
		Branch:  "main",
		IsMain:  true,
		Status:  store.WorktreeStatusActive,
	}
	if err := d.store.UpsertWorktree(wt); err != nil {
		t.Fatal(err)
	}

	found, err := d.findMainRepoPath("myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != mainPath {
		t.Errorf("expected %s, got %s", mainPath, found)
	}
}

func TestFindMainRepoPath_NotFound(t *testing.T) {
	d := newTestDaemon(t)

	_, err := d.findMainRepoPath("nonexistent-project")
	if err == nil {
		t.Fatal("expected error when no main repo found")
	}
	if !strings.Contains(err.Error(), "no main repo") {
		t.Errorf("expected 'no main repo' in error, got: %v", err)
	}
}

func TestFindMainRepoPath_FallbackByProjectName(t *testing.T) {
	d := newTestDaemon(t)

	// Create a worktree that doesn't match project directly but has projectName set
	mainPath := filepath.Join(t.TempDir(), "test-fallback-repo")
	pName := "real-name"
	wt := &store.Worktree{
		Path:        mainPath,
		Project:     "some-other-key",
		Branch:      "main",
		IsMain:      true,
		Status:      store.WorktreeStatusActive,
		ProjectName: &pName,
	}
	if err := d.store.UpsertWorktree(wt); err != nil {
		t.Fatal(err)
	}

	found, err := d.findMainRepoPath("real-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != mainPath {
		t.Errorf("expected %s, got %s", mainPath, found)
	}
}

// --- Orchestrator archetype selection tests ---

func TestHandleSpawn_GoalUsesOrchestratorArchetype(t *testing.T) {
	d := newTestDaemon(t)

	// Create a goal work item
	goal := &store.WorkItem{
		ID:          "wi-goal-arch",
		Project:     "myproject",
		ItemType:    store.WorkItemTypeGoal,
		Subject:     "Build feature X",
		Description: "A high-level goal",
		Status:      store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		WorkItemID: "wi-goal-arch",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)

	// Check that orchestrator archetype was selected
	// The orchestrator uses opus model (from default config)
	foundModel := false
	for i, arg := range resp.ExecArgs {
		if arg == "--model" && i+1 < len(resp.ExecArgs) && resp.ExecArgs[i+1] == "opus" {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Errorf("expected --model opus for orchestrator archetype, args: %v", resp.ExecArgs)
	}

	// Check that the prompt contains orchestrator guidance
	promptIdx := -1
	for i, arg := range resp.ExecArgs {
		if arg == "--append-system-prompt" && i+1 < len(resp.ExecArgs) {
			promptIdx = i + 1
			break
		}
	}
	if promptIdx == -1 {
		t.Fatal("expected --append-system-prompt in ExecArgs")
	}
	prompt := resp.ExecArgs[promptIdx]
	if !strings.Contains(prompt, "Goal Orchestration") {
		t.Error("expected 'Goal Orchestration' in prompt for goal work item")
	}
}

func TestHandleSpawn_FeatureUsesExecutorArchetype(t *testing.T) {
	d := newTestDaemon(t)

	// Register a worktree in the store first (FK constraint)
	wtPath := filepath.Join(t.TempDir(), "test-feature-wt")
	wt := &store.Worktree{
		Path:    wtPath,
		Project: "myproject",
		Branch:  "feat/wi-feature-arch",
		IsMain:  false,
		Status:  store.WorktreeStatusActive,
	}
	if err := d.store.UpsertWorktree(wt); err != nil {
		t.Fatal(err)
	}

	// Create a feature work item
	feature := &store.WorkItem{
		ID:           "wi-feature-arch",
		Project:      "myproject",
		ItemType:     store.WorkItemTypeFeature,
		Subject:      "Implement specific feature",
		Status:       store.WorkItemStatusPending,
		WorktreePath: &wtPath,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatal(err)
	}

	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		FeatureID: "wi-feature-arch",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)

	// Check that executor archetype was selected (uses sonnet model)
	foundModel := false
	for i, arg := range resp.ExecArgs {
		if arg == "--model" && i+1 < len(resp.ExecArgs) && resp.ExecArgs[i+1] == "sonnet" {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Errorf("expected --model sonnet for executor archetype, args: %v", resp.ExecArgs)
	}

	// Should NOT contain orchestrator guidance
	promptIdx := -1
	for i, arg := range resp.ExecArgs {
		if arg == "--append-system-prompt" && i+1 < len(resp.ExecArgs) {
			promptIdx = i + 1
			break
		}
	}
	if promptIdx >= 0 {
		prompt := resp.ExecArgs[promptIdx]
		if strings.Contains(prompt, "Goal Orchestration") {
			t.Error("feature work item should not have Goal Orchestration section")
		}
	}
}

func TestHandleSpawn_ExplicitArchetypeOverride(t *testing.T) {
	d := newTestDaemon(t)

	// Create a goal work item
	goal := &store.WorkItem{
		ID:       "wi-goal-override",
		Project:  "myproject",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Build feature Y",
		Status:   store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	// Explicitly request executor archetype (override default orchestrator)
	reqJSON := mustMarshalSpawnRequest(t, control.SpawnRequest{
		WorkItemID: "wi-goal-override",
		Archetype:  "executor",
	})

	result, err := d.handleSpawn(reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := result.(*control.SpawnResponse)

	// Should use executor (sonnet) not orchestrator (opus)
	foundModel := false
	for i, arg := range resp.ExecArgs {
		if arg == "--model" && i+1 < len(resp.ExecArgs) && resp.ExecArgs[i+1] == "sonnet" {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Errorf("expected --model sonnet when explicitly requesting executor, args: %v", resp.ExecArgs)
	}
}

func TestResolveGoalSpawn_CreatesGoalWorkItem(t *testing.T) {
	d := newTestDaemon(t)

	goalText := "Implement user dashboard"
	wi, parentGoal, ticketCtx, err := d.resolveGoalSpawn(goalText, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create a goal work item
	if wi.ItemType != store.WorkItemTypeGoal {
		t.Errorf("expected goal type, got %s", wi.ItemType)
	}
	if wi.Subject != goalText {
		t.Errorf("expected subject %q, got %q", goalText, wi.Subject)
	}
	if wi.Description != goalText {
		t.Errorf("expected description %q, got %q", goalText, wi.Description)
	}
	if wi.Status != store.WorkItemStatusInProgress {
		t.Errorf("expected in_progress status, got %s", wi.Status)
	}
	if parentGoal != nil {
		t.Errorf("expected nil parent goal for goal spawn, got %+v", parentGoal)
	}
	if ticketCtx != "" {
		t.Errorf("expected empty ticket context, got %q", ticketCtx)
	}

	// Verify persistence
	stored, err := d.store.GetWorkItem(wi.ID)
	if err != nil || stored == nil {
		t.Fatal("goal work item not persisted")
	}
	if stored.ItemType != store.WorkItemTypeGoal {
		t.Errorf("persisted work item should be goal type, got %s", stored.ItemType)
	}
}
