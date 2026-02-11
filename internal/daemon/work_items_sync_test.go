package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/task"
)

// mockProvider implements task.Provider for testing sync without filesystem.
type mockProvider struct {
	tasks map[string][]task.Task // listID -> tasks
}

func (m *mockProvider) Name() string { return "claude" }
func (m *mockProvider) ListTaskLists() ([]task.TaskList, error) {
	var lists []task.TaskList
	for id := range m.tasks {
		lists = append(lists, task.TaskList{ID: id, Provider: "claude"})
	}
	return lists, nil
}
func (m *mockProvider) ListTasks(listID string, _ task.TaskFilters) ([]task.Task, error) {
	return m.tasks[listID], nil
}
func (m *mockProvider) GetTask(listID, taskID string) (*task.Task, error) {
	for _, t := range m.tasks[listID] {
		if t.ID == taskID {
			return &t, nil
		}
	}
	return nil, nil
}
func (m *mockProvider) CreateTask(string, *task.TaskCreate) (*task.Task, error) { return nil, nil }
func (m *mockProvider) UpdateTask(string, string, *task.TaskUpdate) (*task.Task, error) {
	return nil, nil
}
func (m *mockProvider) DeleteTask(string, string) error { return nil }
func (m *mockProvider) Watch(ctx context.Context) (<-chan task.TaskEvent, error) {
	ch := make(chan task.TaskEvent)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}

func newSyncTestDaemon(t *testing.T, mp *mockProvider) *Daemon {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := config.DefaultConfig()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	reg := task.NewRegistry()
	if mp != nil {
		if err := reg.Register(mp); err != nil {
			t.Fatal(err)
		}
	}

	socketPath := filepath.Join(t.TempDir(), "test.sock")
	return &Daemon{
		store:        s,
		config:       cfg,
		server:       control.NewServer(socketPath),
		taskRegistry: reg,
		ctx:          ctx,
		cancel:       cancel,
	}
}

func TestSyncClaudeTasksToWorkItems_GoalChild(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"wi-a1b2": {
				{ID: "1", ListID: "wi-a1b2", Subject: "First task", Status: task.StatusPending},
				{ID: "2", ListID: "wi-a1b2", Subject: "Second task", Status: task.StatusCompleted},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create goal work item
	goal := &store.WorkItem{
		ID:       "wi-a1b2",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Test goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	d.syncClaudeTasksToWorkItems("wi-a1b2", "wi-a1b2")

	// Check synced items
	child1, err := d.store.GetWorkItem("wi-a1b2.1")
	if err != nil {
		t.Fatal(err)
	}
	if child1 == nil {
		t.Fatal("expected synced task wi-a1b2.1")
	}
	if child1.Subject != "First task" {
		t.Errorf("expected subject 'First task', got %q", child1.Subject)
	}
	if child1.Status != store.WorkItemStatusPending {
		t.Errorf("expected pending status, got %s", child1.Status)
	}
	if child1.ParentID == nil || *child1.ParentID != "wi-a1b2" {
		t.Errorf("expected parent ID 'wi-a1b2', got %v", child1.ParentID)
	}

	child2, err := d.store.GetWorkItem("wi-a1b2.2")
	if err != nil {
		t.Fatal(err)
	}
	if child2 == nil {
		t.Fatal("expected synced task wi-a1b2.2")
	}
	if child2.Status != store.WorkItemStatusCompleted {
		t.Errorf("expected completed status, got %s", child2.Status)
	}
}

func TestSyncClaudeTasksToWorkItems_FeatureChild(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			// Directory name wi-a1b2-1 maps to work item ID wi-a1b2.1
			"wi-a1b2-1": {
				{ID: "1", ListID: "wi-a1b2-1", Subject: "Sub-task", Status: task.StatusInProgress},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create parent goal and feature
	goal := &store.WorkItem{
		ID:       "wi-a1b2",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}
	parentID := "wi-a1b2"
	feature := &store.WorkItem{
		ID:       "wi-a1b2.1",
		Project:  "proj",
		ItemType: store.WorkItemTypeFeature,
		ParentID: &parentID,
		Subject:  "Feature",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatal(err)
	}

	// Sync using directory name -> work item ID mapping
	d.syncClaudeTasksToWorkItems("wi-a1b2-1", "wi-a1b2.1")

	child, err := d.store.GetWorkItem("wi-a1b2.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if child == nil {
		t.Fatal("expected synced task wi-a1b2.1.1")
	}
	if child.Subject != "Sub-task" {
		t.Errorf("expected subject 'Sub-task', got %q", child.Subject)
	}
	if child.Status != store.WorkItemStatusInProgress {
		t.Errorf("expected in_progress status, got %s", child.Status)
	}
	if child.ParentID == nil || *child.ParentID != "wi-a1b2.1" {
		t.Errorf("expected parent ID 'wi-a1b2.1', got %v", child.ParentID)
	}
}

func TestSyncClaudeTasksToWorkItems_UpdateExisting(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"wi-a1b2": {
				{ID: "1", ListID: "wi-a1b2", Subject: "Updated subject", Status: task.StatusCompleted},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create goal
	goal := &store.WorkItem{
		ID:       "wi-a1b2",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	// Create existing synced task work item
	parentID := "wi-a1b2"
	existing := &store.WorkItem{
		ID:       "wi-a1b2.1",
		Project:  "proj",
		ItemType: store.WorkItemTypeTask,
		ParentID: &parentID,
		Subject:  "Original subject",
		Status:   store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(existing); err != nil {
		t.Fatal(err)
	}

	d.syncClaudeTasksToWorkItems("wi-a1b2", "wi-a1b2")

	updated, err := d.store.GetWorkItem("wi-a1b2.1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Subject != "Updated subject" {
		t.Errorf("expected updated subject, got %q", updated.Subject)
	}
	if updated.Status != store.WorkItemStatusCompleted {
		t.Errorf("expected completed status, got %s", updated.Status)
	}
}

func TestSyncClaudeTasksToWorkItems_ParentMissing(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"wi-missing": {
				{ID: "1", ListID: "wi-missing", Subject: "Orphan task", Status: task.StatusPending},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// No parent work item in store — sync should silently skip
	d.syncClaudeTasksToWorkItems("wi-missing", "wi-missing")

	// Nothing should be created
	child, err := d.store.GetWorkItem("wi-missing.1")
	if err != nil {
		t.Fatal(err)
	}
	if child != nil {
		t.Errorf("expected no synced task when parent missing, got %+v", child)
	}
}

func TestSyncClaudeTasksToWorkItems_NonWiDirIgnored(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"uuid-1234": {
				{ID: "1", ListID: "uuid-1234", Subject: "Should be ignored"},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// handleTaskEvent filters non-wi- prefixed lists
	event := task.TaskEvent{
		Type:   task.EventTypeCreated,
		ListID: "uuid-1234",
		TaskID: "1",
		Task:   &mp.tasks["uuid-1234"][0],
	}
	d.handleTaskEvent(event)

	// Nothing should be created (no work item lookup even attempted for non-wi)
	child, err := d.store.GetWorkItem("uuid-1234.1")
	if err != nil {
		t.Fatal(err)
	}
	if child != nil {
		t.Error("expected no work item created for non-wi list")
	}
}

func TestSyncClaudeTasksToWorkItems_BatchSync(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"wi-batch": {
				{ID: "1", ListID: "wi-batch", Subject: "Task 1", Status: task.StatusPending},
				{ID: "2", ListID: "wi-batch", Subject: "Task 2", Status: task.StatusInProgress},
				{ID: "3", ListID: "wi-batch", Subject: "Task 3", Status: task.StatusCompleted},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	goal := &store.WorkItem{
		ID:       "wi-batch",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Batch goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	d.syncClaudeTasksToWorkItems("wi-batch", "wi-batch")

	for _, id := range []string{"wi-batch.1", "wi-batch.2", "wi-batch.3"} {
		item, err := d.store.GetWorkItem(id)
		if err != nil {
			t.Fatalf("error getting %s: %v", id, err)
		}
		if item == nil {
			t.Errorf("expected synced task %s", id)
		}
	}
}

func TestSyncClaudeTasksToWorkItems_BlockedByMetadata(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"wi-blocked": {
				{
					ID:        "1",
					ListID:    "wi-blocked",
					Subject:   "Blocked task",
					Status:    task.StatusPending,
					BlockedBy: []string{"2"},
				},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	goal := &store.WorkItem{
		ID:       "wi-blocked",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	d.syncClaudeTasksToWorkItems("wi-blocked", "wi-blocked")

	item, err := d.store.GetWorkItem("wi-blocked.1")
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		t.Fatal("expected synced task")
	}
	if item.Metadata == "" {
		t.Fatal("expected metadata with blocked_by")
	}
	if !isBlockedFromMetadata(item.Metadata) {
		t.Error("expected task to be marked as blocked")
	}
}

func TestHandleTaskEvent_CreatedEvent(t *testing.T) {
	mp := &mockProvider{tasks: map[string][]task.Task{}}
	d := newSyncTestDaemon(t, mp)

	goal := &store.WorkItem{
		ID:       "wi-evt",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	newTask := &task.Task{
		ID:      "1",
		ListID:  "wi-evt",
		Subject: "Created via event",
		Status:  task.StatusPending,
	}
	event := task.TaskEvent{
		Type:   task.EventTypeCreated,
		ListID: "wi-evt",
		TaskID: "1",
		Task:   newTask,
	}
	d.handleTaskEvent(event)

	item, err := d.store.GetWorkItem("wi-evt.1")
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		t.Fatal("expected synced task from created event")
	}
	if item.Subject != "Created via event" {
		t.Errorf("expected subject 'Created via event', got %q", item.Subject)
	}
}

func TestHandleTaskEvent_UpdatedEvent(t *testing.T) {
	mp := &mockProvider{tasks: map[string][]task.Task{}}
	d := newSyncTestDaemon(t, mp)

	goal := &store.WorkItem{
		ID:       "wi-upd",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	// Create existing task
	parentID := "wi-upd"
	existing := &store.WorkItem{
		ID:       "wi-upd.1",
		Project:  "proj",
		ItemType: store.WorkItemTypeTask,
		ParentID: &parentID,
		Subject:  "Original",
		Status:   store.WorkItemStatusPending,
	}
	if err := d.store.CreateWorkItem(existing); err != nil {
		t.Fatal(err)
	}

	updatedTask := &task.Task{
		ID:      "1",
		ListID:  "wi-upd",
		Subject: "Updated via event",
		Status:  task.StatusCompleted,
	}
	event := task.TaskEvent{
		Type:   task.EventTypeUpdated,
		ListID: "wi-upd",
		TaskID: "1",
		Task:   updatedTask,
	}
	d.handleTaskEvent(event)

	item, err := d.store.GetWorkItem("wi-upd.1")
	if err != nil {
		t.Fatal(err)
	}
	if item.Subject != "Updated via event" {
		t.Errorf("expected updated subject, got %q", item.Subject)
	}
	if item.Status != store.WorkItemStatusCompleted {
		t.Errorf("expected completed status, got %s", item.Status)
	}
}

func TestInitialTaskSync_OnlyWiPrefixed(t *testing.T) {
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"wi-sync": {
				{ID: "1", ListID: "wi-sync", Subject: "Synced", Status: task.StatusPending},
			},
			"uuid-ignored": {
				{ID: "1", ListID: "uuid-ignored", Subject: "Ignored", Status: task.StatusPending},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create parent for wi-sync
	goal := &store.WorkItem{
		ID:       "wi-sync",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	d.initialTaskSync()

	// wi-sync should be synced
	item, err := d.store.GetWorkItem("wi-sync.1")
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		t.Fatal("expected wi-sync.1 to be synced")
	}

	// uuid-ignored should NOT be synced
	ignored, err := d.store.GetWorkItem("uuid-ignored.1")
	if err != nil {
		t.Fatal(err)
	}
	if ignored != nil {
		t.Error("expected uuid-prefixed list to be ignored during initial sync")
	}
}

// --- Migration tests ---

func TestMigrateOrphanedTaskLists_DirectLinkage(t *testing.T) {
	// Agent linked to work item via agent_id, UUID task dir should be synced.
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"4adb163b-369d-4c1a-96ba-bf915a69eb5f": {
				{ID: "1", Subject: "Agent task 1", Status: task.StatusCompleted},
				{ID: "2", Subject: "Agent task 2", Status: task.StatusPending},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create agent first (FK: work_items.agent_id references agents.id)
	agent := &store.Agent{
		ID:              "agent-001",
		WorktreePath:    "/tmp/test",
		ProjectName:     "proj",
		Archetype:       "executor",
		Status:          store.AgentStatusCompleted,
		ClaudeSessionID: "4adb163b-369d-4c1a-96ba-bf915a69eb5f",
	}
	if err := d.store.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}

	// Create work item with agent_id
	agentID := "agent-001"
	goal := &store.WorkItem{
		ID:       "wi-test",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Test goal",
		Status:   store.WorkItemStatusInProgress,
		AgentID:  &agentID,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	// Run initial sync (which triggers migration)
	d.initialTaskSync()

	// Tasks should be synced under the work item
	child1, err := d.store.GetWorkItem("wi-test.1")
	if err != nil {
		t.Fatal(err)
	}
	if child1 == nil {
		t.Fatal("expected migrated task wi-test.1")
	}
	if child1.Subject != "Agent task 1" {
		t.Errorf("expected subject 'Agent task 1', got %q", child1.Subject)
	}
	if child1.Status != store.WorkItemStatusCompleted {
		t.Errorf("expected completed status, got %s", child1.Status)
	}

	child2, err := d.store.GetWorkItem("wi-test.2")
	if err != nil {
		t.Fatal(err)
	}
	if child2 == nil {
		t.Fatal("expected migrated task wi-test.2")
	}
}

func TestMigrateOrphanedTaskLists_WorktreePathExtraction(t *testing.T) {
	// Agent not directly linked to work item, but worktree path contains the ID.
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"bc47eaee-f3b1-4168-9866-4b8655152332": {
				{ID: "1", Subject: "Feature task", Status: task.StatusCompleted},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create parent goal and feature (no agent_id set on either)
	goal := &store.WorkItem{
		ID:       "wi-c5a6",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Parent goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}
	parentID := "wi-c5a6"
	feature := &store.WorkItem{
		ID:       "wi-c5a6.1",
		Project:  "proj",
		ItemType: store.WorkItemTypeFeature,
		ParentID: &parentID,
		Subject:  "Feature",
		Status:   store.WorkItemStatusCompleted,
	}
	if err := d.store.CreateWorkItem(feature); err != nil {
		t.Fatal(err)
	}

	// Create agent with worktree path that contains the feature ID
	agent := &store.Agent{
		ID:              "agent-wt",
		WorktreePath:    "/Users/drew/repos/worktrees/wi-c5a6.1-1925",
		ProjectName:     "proj",
		Archetype:       "executor",
		Status:          store.AgentStatusCompleted,
		ClaudeSessionID: "bc47eaee-f3b1-4168-9866-4b8655152332",
	}
	if err := d.store.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}

	d.initialTaskSync()

	// Task should be synced under the feature
	child, err := d.store.GetWorkItem("wi-c5a6.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if child == nil {
		t.Fatal("expected migrated task wi-c5a6.1.1")
	}
	if child.Subject != "Feature task" {
		t.Errorf("expected subject 'Feature task', got %q", child.Subject)
	}
	if child.ParentID == nil || *child.ParentID != "wi-c5a6.1" {
		t.Errorf("expected parent ID 'wi-c5a6.1', got %v", child.ParentID)
	}
}

func TestMigrateOrphanedTaskLists_NoMatchingAgent(t *testing.T) {
	// UUID dir with no matching agent should be ignored.
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"deadbeef-0000-0000-0000-000000000000": {
				{ID: "1", Subject: "Orphan", Status: task.StatusPending},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	d.initialTaskSync()

	// Nothing should be created
	items, err := d.store.ListWorkItems("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected no work items, got %d", len(items))
	}
}

func TestMigrateOrphanedTaskLists_WorkItemMissing(t *testing.T) {
	// Agent maps to a work item that doesn't exist in store — skip gracefully.
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee": {
				{ID: "1", Subject: "Task", Status: task.StatusPending},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	// Create agent first (FK constraint)
	agentID := "agent-ghost"
	agent := &store.Agent{
		ID:              agentID,
		WorktreePath:    "/tmp/test",
		ProjectName:     "proj",
		Archetype:       "executor",
		Status:          store.AgentStatusCompleted,
		ClaudeSessionID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	if err := d.store.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}

	// Create work item linked to agent, then delete it
	ghost := &store.WorkItem{
		ID:       "wi-gone",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Will be deleted",
		Status:   store.WorkItemStatusPending,
		AgentID:  &agentID,
	}
	if err := d.store.CreateWorkItem(ghost); err != nil {
		t.Fatal(err)
	}

	// Delete the work item so migration finds it missing
	if err := d.store.DeleteWorkItem("wi-gone"); err != nil {
		t.Fatal(err)
	}

	// Should not panic or create orphaned tasks
	d.initialTaskSync()

	child, err := d.store.GetWorkItem("wi-gone.1")
	if err != nil {
		t.Fatal(err)
	}
	if child != nil {
		t.Error("expected no task synced when parent work item is missing")
	}
}

func TestMigrateOrphanedTaskLists_NonUUIDSkipped(t *testing.T) {
	// Non-UUID task dirs should not be affected by migration.
	mp := &mockProvider{
		tasks: map[string][]task.Task{
			"v1-push": {
				{ID: "1", Subject: "v1 task", Status: task.StatusPending},
			},
			"wi-sync": {
				{ID: "1", Subject: "wi task", Status: task.StatusPending},
			},
		},
	}
	d := newSyncTestDaemon(t, mp)

	goal := &store.WorkItem{
		ID:       "wi-sync",
		Project:  "proj",
		ItemType: store.WorkItemTypeGoal,
		Subject:  "Goal",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(goal); err != nil {
		t.Fatal(err)
	}

	d.initialTaskSync()

	// wi-sync task should be synced (normal path)
	item, err := d.store.GetWorkItem("wi-sync.1")
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		t.Fatal("expected wi-sync.1 to be synced")
	}

	// v1-push should NOT create any work items (not UUID, not wi-)
	v1Item, err := d.store.GetWorkItem("v1-push.1")
	if err != nil {
		t.Fatal(err)
	}
	if v1Item != nil {
		t.Error("expected v1-push task to NOT be synced")
	}
}
