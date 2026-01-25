package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/task"
)

// Task handlers for Claude Code task integration

func (d *Daemon) handleListTaskProviders(_ json.RawMessage) (any, error) {
	if d.taskRegistry == nil {
		return []string{}, nil
	}
	return d.taskRegistry.ProviderNames(), nil
}

func (d *Daemon) handleListTaskLists(_ json.RawMessage) (any, error) {
	if d.taskRegistry == nil {
		return []*control.TaskListInfo{}, nil
	}

	lists, err := d.taskRegistry.ListAllTaskLists()
	if err != nil {
		return nil, err
	}

	var result []*control.TaskListInfo
	for _, l := range lists {
		result = append(result, taskListToInfo(&l))
	}
	return result, nil
}

func (d *Daemon) handleListTasks(params json.RawMessage) (any, error) {
	var req struct {
		Provider string         `json:"provider"`
		ListID   string         `json:"list_id"`
		Status   *string        `json:"status,omitempty"`
		Owner    *string        `json:"owner,omitempty"`
		Blocked  *bool          `json:"blocked,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return []*control.TaskInfo{}, nil
	}

	// Default provider to "claude" if not specified
	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	var filters task.TaskFilters
	if req.Status != nil {
		s := task.Status(*req.Status)
		filters.Status = &s
	}
	filters.Owner = req.Owner
	filters.Blocked = req.Blocked

	tasks, err := d.taskRegistry.ListTasks(provider, req.ListID, filters)
	if err != nil {
		return nil, err
	}

	var result []*control.TaskInfo
	for _, t := range tasks {
		result = append(result, taskToInfo(&t))
	}
	return result, nil
}

func (d *Daemon) handleGetTask(params json.RawMessage) (any, error) {
	var req struct {
		Provider string `json:"provider"`
		ListID   string `json:"list_id"`
		TaskID   string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return nil, fmt.Errorf("task registry not initialized")
	}

	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	t, err := d.taskRegistry.GetTask(provider, req.ListID, req.TaskID)
	if err != nil {
		return nil, err
	}

	return taskToInfo(t), nil
}

func (d *Daemon) handleCreateTask(params json.RawMessage) (any, error) {
	var req struct {
		Provider    string         `json:"provider"`
		ListID      string         `json:"list_id"`
		Subject     string         `json:"subject"`
		Description string         `json:"description,omitempty"`
		Status      string         `json:"status,omitempty"`
		ActiveForm  string         `json:"active_form,omitempty"`
		Blocks      []string       `json:"blocks,omitempty"`
		BlockedBy   []string       `json:"blocked_by,omitempty"`
		Metadata    map[string]any `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return nil, fmt.Errorf("task registry not initialized")
	}

	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	create := &task.TaskCreate{
		Subject:     req.Subject,
		Description: req.Description,
		ActiveForm:  req.ActiveForm,
		Blocks:      req.Blocks,
		BlockedBy:   req.BlockedBy,
		Metadata:    req.Metadata,
	}
	if req.Status != "" {
		create.Status = task.Status(req.Status)
	}

	t, err := d.taskRegistry.CreateTask(provider, req.ListID, create)
	if err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "task_created",
		Payload: taskToInfo(t),
	})

	return taskToInfo(t), nil
}

func (d *Daemon) handleUpdateTask(params json.RawMessage) (any, error) {
	var req struct {
		Provider     string         `json:"provider"`
		ListID       string         `json:"list_id"`
		TaskID       string         `json:"task_id"`
		Subject      *string        `json:"subject,omitempty"`
		Description  *string        `json:"description,omitempty"`
		Status       *string        `json:"status,omitempty"`
		ActiveForm   *string        `json:"active_form,omitempty"`
		Owner        *string        `json:"owner,omitempty"`
		AddBlocks    []string       `json:"add_blocks,omitempty"`
		AddBlockedBy []string       `json:"add_blocked_by,omitempty"`
		Metadata     map[string]any `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return nil, fmt.Errorf("task registry not initialized")
	}

	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	update := &task.TaskUpdate{
		Subject:      req.Subject,
		Description:  req.Description,
		ActiveForm:   req.ActiveForm,
		Owner:        req.Owner,
		AddBlocks:    req.AddBlocks,
		AddBlockedBy: req.AddBlockedBy,
		Metadata:     req.Metadata,
	}
	if req.Status != nil {
		s := task.Status(*req.Status)
		update.Status = &s
	}

	t, err := d.taskRegistry.UpdateTask(provider, req.ListID, req.TaskID, update)
	if err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "task_updated",
		Payload: taskToInfo(t),
	})

	return taskToInfo(t), nil
}

func (d *Daemon) handleDeleteTask(params json.RawMessage) (any, error) {
	var req struct {
		Provider string `json:"provider"`
		ListID   string `json:"list_id"`
		TaskID   string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return nil, fmt.Errorf("task registry not initialized")
	}

	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	if err := d.taskRegistry.DeleteTask(provider, req.ListID, req.TaskID); err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type: "task_deleted",
		Payload: map[string]string{
			"provider": provider,
			"list_id":  req.ListID,
			"task_id":  req.TaskID,
		},
	})

	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleExecuteTask(params json.RawMessage) (any, error) {
	var req struct {
		Provider     string `json:"provider"`
		ListID       string `json:"list_id"`
		TaskID       string `json:"task_id"`
		WorktreePath string `json:"worktree_path"`
		Archetype    string `json:"archetype,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return nil, fmt.Errorf("task registry not initialized")
	}

	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	// Get the task
	t, err := d.taskRegistry.GetTask(provider, req.ListID, req.TaskID)
	if err != nil {
		return nil, err
	}

	// Get worktree info
	wt, err := d.store.GetWorktree(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	if wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", req.WorktreePath)
	}

	// Build prompt from task
	prompt := fmt.Sprintf("## Task: %s\n\n%s", t.Subject, t.Description)

	archetype := req.Archetype
	if archetype == "" {
		archetype = "executor"
	}

	// Spawn agent with task context
	spec := agent.SpawnSpec{
		WorktreePath: req.WorktreePath,
		ProjectName:  wt.Project,
		Archetype:    archetype,
		Prompt:       prompt,
		TaskListID:   req.ListID, // Set CLAUDE_CODE_TASK_LIST_ID
	}

	spawnedAgent, err := d.spawner.Spawn(d.ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Update task status to in_progress
	inProgress := task.StatusInProgress
	d.taskRegistry.UpdateTask(provider, req.ListID, req.TaskID, &task.TaskUpdate{
		Status: &inProgress,
		Owner:  &spawnedAgent.ID,
	})

	// Associate agent with worktree
	d.store.AssignAgentToWorktree(req.WorktreePath, spawnedAgent.ID)

	// Broadcast events
	d.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: d.agentToInfo(spawnedAgent),
	})

	return d.agentToInfo(spawnedAgent), nil
}

func (d *Daemon) handleBroadcastTask(params json.RawMessage) (any, error) {
	var req struct {
		Provider string `json:"provider"`
		ListID   string `json:"list_id"`
		TaskID   string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if d.taskRegistry == nil {
		return nil, fmt.Errorf("task registry not initialized")
	}

	provider := req.Provider
	if provider == "" {
		provider = "claude"
	}

	// Get the task
	t, err := d.taskRegistry.GetTask(provider, req.ListID, req.TaskID)
	if err != nil {
		return nil, err
	}

	// Broadcast task to all connected clients
	d.server.Broadcast(control.Event{
		Type:    "task_broadcast",
		Payload: taskToInfo(t),
	})

	return map[string]bool{"success": true}, nil
}

// Helper functions

func taskListToInfo(l *task.TaskList) *control.TaskListInfo {
	return &control.TaskListInfo{
		ID:        l.ID,
		Name:      l.Name,
		Provider:  l.Provider,
		Path:      l.Path,
		TaskCount: l.TaskCount,
		CreatedAt: l.CreatedAt.Format(time.RFC3339),
		UpdatedAt: l.UpdatedAt.Format(time.RFC3339),
	}
}

func taskToInfo(t *task.Task) *control.TaskInfo {
	return &control.TaskInfo{
		ID:          t.ID,
		ListID:      t.ListID,
		Subject:     t.Subject,
		Description: t.Description,
		Status:      string(t.Status),
		ActiveForm:  t.ActiveForm,
		Owner:       t.Owner,
		Blocks:      t.Blocks,
		BlockedBy:   t.BlockedBy,
		Metadata:    t.Metadata,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
	}
}
