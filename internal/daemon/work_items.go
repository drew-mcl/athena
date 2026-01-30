package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/task"
)

// Work item handlers

func (d *Daemon) handleListWorkItems(params json.RawMessage) (any, error) {
	var req control.ListWorkItemsRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
	}

	items, err := d.store.ListWorkItems(
		req.Project,
		store.WorkItemType(req.ItemType),
		store.WorkItemStatus(req.Status),
	)
	if err != nil {
		return nil, err
	}

	var result []*control.WorkItemInfo
	for _, item := range items {
		info := workItemToInfo(item)
		// Add progress for goals and features
		if item.ItemType == store.WorkItemTypeGoal || item.ItemType == store.WorkItemTypeFeature {
			completed, total, _ := d.store.GetWorkItemProgress(item.ID)
			info.CompletedCount = completed
			info.TotalCount = total
		}
		result = append(result, info)
	}
	return result, nil
}

func (d *Daemon) handleGetWorkItem(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	item, err := d.store.GetWorkItem(req.ID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("work item not found: %s", req.ID)
	}

	info := workItemToInfo(item)

	// Add progress
	completed, total, _ := d.store.GetWorkItemProgress(item.ID)
	info.CompletedCount = completed
	info.TotalCount = total

	return info, nil
}

func (d *Daemon) handleCreateWorkItem(params json.RawMessage) (any, error) {
	var req control.CreateWorkItemRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Generate ID
	id, err := d.store.GenerateWorkItemID(req.ParentID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ID: %w", err)
	}

	// Default status
	status := store.WorkItemStatus(req.Status)
	if status == "" {
		status = store.WorkItemStatusPending
	}

	item := &store.WorkItem{
		ID:          id,
		Project:     req.Project,
		ItemType:    store.WorkItemType(req.ItemType),
		ParentID:    nilIfEmpty(req.ParentID),
		Subject:     req.Subject,
		Description: req.Description,
		Status:      status,
		TicketID:    nilIfEmpty(req.TicketID),
		Priority:    store.WorkItemPriority(req.Priority),
	}

	if err := d.store.CreateWorkItem(item); err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "work_item_created",
		Payload: workItemToInfo(item),
	})

	return workItemToInfo(item), nil
}

func (d *Daemon) handleUpdateWorkItem(params json.RawMessage) (any, error) {
	var req control.UpdateWorkItemRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	item, err := d.store.GetWorkItem(req.ID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("work item not found: %s", req.ID)
	}

	// Apply updates
	if req.Subject != "" {
		item.Subject = req.Subject
	}
	if req.Description != "" {
		item.Description = req.Description
	}
	if req.Status != "" {
		item.Status = store.WorkItemStatus(req.Status)
	}
	if req.Priority != nil {
		item.Priority = store.WorkItemPriority(*req.Priority)
	}
	if req.AgentID != "" {
		item.AgentID = &req.AgentID
	}
	if req.WorktreePath != "" {
		item.WorktreePath = &req.WorktreePath
	}

	if err := d.store.UpdateWorkItem(item); err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "work_item_updated",
		Payload: workItemToInfo(item),
	})

	return workItemToInfo(item), nil
}

func (d *Daemon) handleDeleteWorkItem(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.DeleteWorkItem(req.ID); err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type: "work_item_deleted",
		Payload: map[string]string{
			"id": req.ID,
		},
	})

	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleGetWorkItemTree(params json.RawMessage) (any, error) {
	var req struct {
		RootID  string `json:"root_id"`
		Project string `json:"project"` // If root_id empty, get all for project
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	var items []*store.WorkItem
	var err error

	if req.RootID != "" {
		// Get tree from specific root
		items, err = d.store.GetWorkItemTree(req.RootID)
	} else {
		// Get all goals for project
		goals, err := d.store.ListWorkItems(req.Project, store.WorkItemTypeGoal, "")
		if err != nil {
			return nil, err
		}

		// For each goal, get its full tree
		for _, goal := range goals {
			tree, _ := d.store.GetWorkItemTree(goal.ID)
			items = append(items, tree...)
		}

		// Also get orphan tasks
		orphans, _ := d.store.ListOrphanTasks(req.Project)
		items = append(items, orphans...)
	}

	if err != nil {
		return nil, err
	}

	var result []*control.WorkItemInfo
	for _, item := range items {
		info := workItemToInfo(item)
		// Add progress for non-tasks
		if item.ItemType != store.WorkItemTypeTask {
			completed, total, _ := d.store.GetWorkItemProgress(item.ID)
			info.CompletedCount = completed
			info.TotalCount = total
		}
		result = append(result, info)
	}

	return result, nil
}

func (d *Daemon) handleGetWorkItemChildren(params json.RawMessage) (any, error) {
	var req struct {
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	items, err := d.store.ListWorkItemsByParent(req.ParentID)
	if err != nil {
		return nil, err
	}

	var result []*control.WorkItemInfo
	for _, item := range items {
		info := workItemToInfo(item)
		// Add progress for non-tasks
		if item.ItemType != store.WorkItemTypeTask {
			completed, total, _ := d.store.GetWorkItemProgress(item.ID)
			info.CompletedCount = completed
			info.TotalCount = total
		}
		result = append(result, info)
	}

	return result, nil
}

func (d *Daemon) handleGetWorkItemAncestors(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	items, err := d.store.GetWorkItemAncestors(req.ID)
	if err != nil {
		return nil, err
	}

	var result []*control.WorkItemInfo
	for _, item := range items {
		result = append(result, workItemToInfo(item))
	}

	return result, nil
}

func (d *Daemon) handleGetReadyItems(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if params != nil {
		json.Unmarshal(params, &req)
	}

	items, err := d.store.ListReadyItems(req.Project)
	if err != nil {
		return nil, err
	}

	var result []*control.WorkItemInfo
	for _, item := range items {
		result = append(result, workItemToInfo(item))
	}

	return result, nil
}

// Helper functions

func workItemToInfo(item *store.WorkItem) *control.WorkItemInfo {
	info := &control.WorkItemInfo{
		ID:          item.ID,
		Project:     item.Project,
		ItemType:    string(item.ItemType),
		Subject:     item.Subject,
		Description: item.Description,
		Status:      string(item.Status),
		Priority:    int(item.Priority),
		CreatedAt:   item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   item.UpdatedAt.Format(time.RFC3339),
	}

	if item.ParentID != nil {
		info.ParentID = *item.ParentID
	}
	if item.WorktreePath != nil {
		info.WorktreePath = *item.WorktreePath
	}
	if item.TicketID != nil {
		info.TicketID = *item.TicketID
	}
	if item.PRURL != nil {
		info.PRURL = *item.PRURL
	}
	if item.AgentID != nil {
		info.AgentID = *item.AgentID
	}

	return info
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Task sync watcher - syncs Claude Code tasks to work_items table

// startTaskWatcher begins watching for Claude task changes and syncing to work_items.
func (d *Daemon) startTaskWatcher(ctx context.Context) {
	events, err := d.taskRegistry.WatchAll(ctx)
	if err != nil {
		logging.Warn("failed to start task watcher", "error", err)
		return
	}

	d.safeGo("task-watcher", func() {
		for event := range events {
			d.handleTaskEvent(event)
		}
	})

	logging.Info("task watcher started")
}

// handleTaskEvent processes a task event and syncs to work_items if applicable.
func (d *Daemon) handleTaskEvent(event task.TaskEvent) {
	// Only sync work items (wi-xxxx pattern)
	if !strings.HasPrefix(event.ListID, "wi-") {
		return
	}

	switch event.Type {
	case task.EventTypeListSync:
		// Full resync of a task list
		d.syncClaudeTasksToWorkItems(event.ListID)

	case task.EventTypeCreated, task.EventTypeUpdated:
		if event.Task != nil {
			d.syncSingleTask(event.ListID, event.Task)
		}

	case task.EventTypeDeleted:
		// Mark task as deleted (soft delete in work_items)
		if event.TaskID != "" {
			taskWorkItemID := fmt.Sprintf("%s.%s", event.ListID, event.TaskID)
			d.store.DeleteWorkItem(taskWorkItemID)
		}
	}

	// Broadcast update to TUI clients
	d.server.Broadcast(control.Event{
		Type: "work_items_synced",
		Payload: map[string]string{
			"list_id": event.ListID,
		},
	})
}

// syncClaudeTasksToWorkItems syncs all tasks from a Claude task list to work_items.
func (d *Daemon) syncClaudeTasksToWorkItems(listID string) {
	// Get the parent work item
	parent, err := d.store.GetWorkItem(listID)
	if err != nil || parent == nil {
		logging.Debug("work item not found for task sync", "list_id", listID)
		return
	}

	// Get tasks from Claude
	tasks, err := d.taskRegistry.ListTasks("claude", listID, task.TaskFilters{})
	if err != nil {
		logging.Debug("failed to list tasks for sync", "list_id", listID, "error", err)
		return
	}

	// Sync each task
	for _, t := range tasks {
		d.syncSingleTask(listID, &t)
	}

	logging.Debug("synced Claude tasks to work_items",
		"list_id", listID,
		"task_count", len(tasks))
}

// syncSingleTask syncs a single Claude task to a work_item.
func (d *Daemon) syncSingleTask(listID string, t *task.Task) {
	// Get parent work item for project context
	parent, err := d.store.GetWorkItem(listID)
	if err != nil || parent == nil {
		return
	}

	// Task work item ID: parent.taskID (e.g., wi-a3f8.1.task-123)
	taskWorkItemID := fmt.Sprintf("%s.%s", listID, t.ID)

	// Check if task work item exists
	existing, _ := d.store.GetWorkItem(taskWorkItemID)

	// Map task status to work item status
	status := store.WorkItemStatusPending
	switch t.Status {
	case task.StatusInProgress:
		status = store.WorkItemStatusInProgress
	case task.StatusCompleted:
		status = store.WorkItemStatusCompleted
	}

	if existing == nil {
		// Create new work item for this task
		item := &store.WorkItem{
			ID:          taskWorkItemID,
			Project:     parent.Project,
			ItemType:    store.WorkItemTypeTask,
			ParentID:    &listID,
			Subject:     t.Subject,
			Description: t.Description,
			Status:      status,
			Priority:    store.WorkItemPriorityNormal,
		}
		if t.Owner != "" {
			item.AgentID = &t.Owner
		}
		if err := d.store.CreateWorkItem(item); err != nil {
			logging.Debug("failed to create synced work item", "id", taskWorkItemID, "error", err)
		}
	} else {
		// Update existing work item
		existing.Subject = t.Subject
		existing.Description = t.Description
		existing.Status = status
		if t.Owner != "" {
			existing.AgentID = &t.Owner
		}
		if err := d.store.UpdateWorkItem(existing); err != nil {
			logging.Debug("failed to update synced work item", "id", taskWorkItemID, "error", err)
		}
	}
}
