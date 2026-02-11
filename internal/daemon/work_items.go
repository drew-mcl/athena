package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

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

	return d.workItemsToInfo(items, includeGoalFeatureProgress), nil
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
	completed, total, err := d.store.GetWorkItemProgress(item.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load work item progress: %w", err)
	}
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
			tree, err := d.store.GetWorkItemTree(goal.ID)
			if err != nil {
				return nil, err
			}
			items = append(items, tree...)
		}

		// Also get orphan tasks
		orphans, err := d.store.ListOrphanTasks(req.Project)
		if err != nil {
			return nil, err
		}
		items = append(items, orphans...)
	}

	if err != nil {
		return nil, err
	}

	return d.workItemsToInfo(items, includeNonTaskProgress), nil
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

	return d.workItemsToInfo(items, includeNonTaskProgress), nil
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

	return d.workItemsToInfo(items, nil), nil
}

func (d *Daemon) handleGetReadyItems(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
	}

	items, err := d.store.ListReadyItems(req.Project)
	if err != nil {
		return nil, err
	}

	return d.workItemsToInfo(items, nil), nil
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

	// Check metadata for blocked_by
	if item.Metadata != "" && item.Status != store.WorkItemStatusCompleted {
		info.Blocked = isBlockedFromMetadata(item.Metadata)
	}

	return info
}

func includeGoalFeatureProgress(itemType store.WorkItemType) bool {
	return itemType == store.WorkItemTypeGoal || itemType == store.WorkItemTypeFeature
}

func includeNonTaskProgress(itemType store.WorkItemType) bool {
	return itemType != store.WorkItemTypeTask
}

func (d *Daemon) workItemsToInfo(items []*store.WorkItem, includeProgress func(store.WorkItemType) bool) []*control.WorkItemInfo {
	result := make([]*control.WorkItemInfo, 0, len(items))
	for _, item := range items {
		info := workItemToInfo(item)
		if includeProgress != nil && includeProgress(item.ItemType) {
			completed, total, err := d.store.GetWorkItemProgress(item.ID)
			if err != nil {
				logging.Debug("failed to load work item progress", "id", item.ID, "error", err)
			} else {
				info.CompletedCount = completed
				info.TotalCount = total
			}
		}
		result = append(result, info)
	}
	return result
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
	// Initial sync: read all existing task lists and sync matching work items
	d.initialTaskSync()

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

// initialTaskSync syncs all existing Claude task lists to work_items on daemon startup.
func (d *Daemon) initialTaskSync() {
	if d.taskRegistry == nil {
		return
	}

	lists, err := d.taskRegistry.ListAllTaskLists()
	if err != nil {
		logging.Debug("failed to list task lists for initial sync", "error", err)
		return
	}

	// First, migrate orphaned UUID task lists to their correct work items.
	d.migrateOrphanedTaskLists(lists)

	synced := 0
	for _, l := range lists {
		if !strings.HasPrefix(l.ID, "wi-") {
			continue
		}
		taskListID := l.ID
		workItemID := taskListDirToWorkItemID(taskListID)
		d.syncClaudeTasksToWorkItems(taskListID, workItemID)
		synced++
	}

	if synced > 0 {
		logging.Info("initial task sync complete", "lists_synced", synced)
	}
}

// uuidPattern matches UUID-format strings (e.g., "4adb163b-369d-4c1a-96ba-bf915a69eb5f").
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// migrateOrphanedTaskLists finds UUID-named task directories that should belong
// to work items (via agents table) and syncs their tasks to the correct work_items.
//
// Before the env var pipeline was fixed, Claude Code would ignore the
// CLAUDE_CODE_TASK_LIST_ID env and create its own UUID-based task list.
// This migration reads those orphaned lists and syncs them to the correct parent.
func (d *Daemon) migrateOrphanedTaskLists(lists []task.TaskList) {
	// Build set of UUID task list IDs.
	var uuidLists []string
	for _, l := range lists {
		if uuidPattern.MatchString(l.ID) {
			uuidLists = append(uuidLists, l.ID)
		}
	}
	if len(uuidLists) == 0 {
		return
	}

	// Build mapping: UUID (claude_session_id) → work item ID.
	mapping := d.buildSessionToWorkItemMapping()
	if len(mapping) == 0 {
		return
	}

	migrated := 0
	for _, uuid := range uuidLists {
		workItemID, ok := mapping[uuid]
		if !ok {
			continue
		}

		// Verify the work item exists in the store.
		wi, err := d.store.GetWorkItem(workItemID)
		if err != nil || wi == nil {
			logging.Debug("skipping orphaned task list migration: work item not found",
				"uuid", uuid, "work_item_id", workItemID)
			continue
		}

		// Sync tasks from the UUID dir to the correct work item.
		// The UUID is used as the task list ID (directory name) for file reads,
		// while workItemID is used for store lookups.
		d.syncClaudeTasksToWorkItems(uuid, workItemID)
		migrated++
		logging.Info("migrated orphaned task list",
			"uuid", uuid, "work_item_id", workItemID, "project", wi.Project)
	}

	if migrated > 0 {
		logging.Info("orphaned task list migration complete", "migrated", migrated)
	}
}

// buildSessionToWorkItemMapping builds a map from claude_session_id (UUID) to work item ID.
// It uses two strategies:
//  1. Direct: agent linked to work_item via work_items.agent_id
//  2. Worktree path: extract work item ID from worktree directory basename
func (d *Daemon) buildSessionToWorkItemMapping() map[string]string {
	agents, err := d.store.ListAgents()
	if err != nil {
		logging.Debug("failed to list agents for task migration", "error", err)
		return nil
	}

	// Pre-load work items that have agent_id set for direct linkage.
	agentToWorkItem := make(map[string]string) // agent.ID → work_item.ID
	allWorkItems, err := d.store.ListWorkItems("", "", "")
	if err != nil {
		logging.Debug("failed to list work items for task migration", "error", err)
		return nil
	}
	for _, wi := range allWorkItems {
		if wi.AgentID != nil && *wi.AgentID != "" {
			agentToWorkItem[*wi.AgentID] = wi.ID
		}
	}

	mapping := make(map[string]string)
	for _, agent := range agents {
		sessionID := agent.ClaudeSessionID
		if sessionID == "" || !uuidPattern.MatchString(sessionID) {
			continue
		}

		// Strategy 1: Direct linkage via agent_id on work_items.
		if wiID, ok := agentToWorkItem[agent.ID]; ok {
			mapping[sessionID] = wiID
			continue
		}

		// Strategy 2: Extract work item ID from worktree path basename.
		// Worktree paths follow: <base>/<work-item-id>-<4char-hash>
		// e.g., /Users/drew/repos/worktrees/wi-c5a6.1-1925 → wi-c5a6.1
		if wiID := extractWorkItemIDFromWorktreePath(agent.WorktreePath); wiID != "" {
			mapping[sessionID] = wiID
		}
	}

	return mapping
}

// extractWorkItemIDFromWorktreePath extracts a work item ID from a worktree directory path.
// Worktree dirs are named "<work-item-id>-<4char-hash>" (e.g., "wi-c5a6.1-1925").
// Returns empty string if the path doesn't match the expected pattern.
func extractWorkItemIDFromWorktreePath(wtPath string) string {
	if wtPath == "" {
		return ""
	}
	base := filepath.Base(wtPath)
	if !strings.HasPrefix(base, "wi-") {
		return ""
	}

	// Find the last hyphen — the suffix after it should be a 4-char hex hash.
	lastHyphen := strings.LastIndex(base, "-")
	if lastHyphen < 0 || lastHyphen <= 2 { // must be after "wi-"
		return ""
	}

	suffix := base[lastHyphen+1:]
	if len(suffix) != 4 || !isHex(suffix) {
		return ""
	}

	return base[:lastHyphen]
}

// isHex returns true if s consists entirely of hexadecimal characters.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

// handleTaskEvent processes a task event and syncs to work_items if applicable.
func (d *Daemon) handleTaskEvent(event task.TaskEvent) {
	// Only sync work items (wi-xxxx pattern)
	if !strings.HasPrefix(event.ListID, "wi-") {
		return
	}

	// Claude Code converts dots to hyphens in directory names (e.g., wi-266d.2 -> wi-266d-2).
	// Convert back: the task list directory name is used for file reads, while the work item
	// ID (with dots) is used for store lookups.
	taskListID := event.ListID
	workItemID := taskListDirToWorkItemID(taskListID)

	switch event.Type {
	case task.EventTypeListSync:
		// Full resync of a task list
		d.syncClaudeTasksToWorkItems(taskListID, workItemID)

	case task.EventTypeCreated, task.EventTypeUpdated:
		if event.Task != nil {
			d.syncSingleTask(taskListID, workItemID, event.Task)
		}

	case task.EventTypeDeleted:
		// No-op: keep synced work items for reference even after Claude cleans up task files.
		// The task was already synced with its final status.
		logging.Debug("task file deleted, keeping synced work item",
			"list_id", event.ListID, "task_id", event.TaskID)
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
// taskListID is the directory name (e.g., wi-266d-2) used for file reads.
// workItemID is the store ID (e.g., wi-266d.2) used for store lookups.
func (d *Daemon) syncClaudeTasksToWorkItems(taskListID, workItemID string) {
	// Get the parent work item
	parent, err := d.store.GetWorkItem(workItemID)
	if err != nil {
		logging.Debug("failed to load work item for task sync", "task_list_id", taskListID, "work_item_id", workItemID, "error", err)
		return
	}
	if parent == nil {
		logging.Debug("work item not found for task sync", "task_list_id", taskListID, "work_item_id", workItemID)
		return
	}

	// Get tasks from Claude (uses directory name)
	tasks, err := d.taskRegistry.ListTasks("claude", taskListID, task.TaskFilters{})
	if err != nil {
		logging.Debug("failed to list tasks for sync", "task_list_id", taskListID, "error", err)
		return
	}

	// Sync each task
	for _, t := range tasks {
		d.syncSingleTask(taskListID, workItemID, &t)
	}

	logging.Debug("synced Claude tasks to work_items",
		"task_list_id", taskListID,
		"work_item_id", workItemID,
		"task_count", len(tasks))
}

// syncSingleTask syncs a single Claude task to a work_item.
// taskListID is the directory name for file reads; workItemID is the store ID.
func (d *Daemon) syncSingleTask(taskListID, workItemID string, t *task.Task) {
	// Get parent work item for project context
	parent, err := d.store.GetWorkItem(workItemID)
	if err != nil {
		logging.Debug("failed to load parent work item for task sync", "work_item_id", workItemID, "error", err)
		return
	}
	if parent == nil {
		return
	}

	// Task work item ID: parent.taskID (e.g., wi-a3f8.1.1)
	taskWorkItemID := fmt.Sprintf("%s.%s", workItemID, t.ID)

	// Check if task work item exists
	existing, err := d.store.GetWorkItem(taskWorkItemID)
	if err != nil {
		logging.Debug("failed to load existing task work item", "id", taskWorkItemID, "error", err)
		return
	}

	// Map task status to work item status
	status := store.WorkItemStatusPending
	switch t.Status {
	case task.StatusInProgress:
		status = store.WorkItemStatusInProgress
	case task.StatusCompleted:
		status = store.WorkItemStatusCompleted
	}

	// Build metadata JSON with blocked_by info
	metadata := buildTaskMetadata(t)

	if existing == nil {
		// Create new work item for this task
		item := &store.WorkItem{
			ID:          taskWorkItemID,
			Project:     parent.Project,
			ItemType:    store.WorkItemTypeTask,
			ParentID:    &workItemID,
			Subject:     t.Subject,
			Description: t.Description,
			Status:      status,
			Priority:    store.WorkItemPriorityNormal,
			Metadata:    metadata,
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
		existing.Metadata = metadata
		if t.Owner != "" {
			existing.AgentID = &t.Owner
		}
		if err := d.store.UpdateWorkItem(existing); err != nil {
			logging.Debug("failed to update synced work item", "id", taskWorkItemID, "error", err)
		}
	}
}

// buildTaskMetadata creates a JSON metadata string from a Claude task,
// preserving blocked_by information.
func buildTaskMetadata(t *task.Task) string {
	if len(t.BlockedBy) == 0 {
		return ""
	}
	meta := map[string]any{
		"blocked_by": t.BlockedBy,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(data)
}

// isBlockedFromMetadata checks if a work item's metadata JSON contains
// a non-empty blocked_by list.
func isBlockedFromMetadata(metadata string) bool {
	var meta map[string]any
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return false
	}
	blockedBy, ok := meta["blocked_by"]
	if !ok {
		return false
	}
	arr, ok := blockedBy.([]any)
	return ok && len(arr) > 0
}

// taskListDirToWorkItemID converts a Claude Code task list directory name to an
// Athena work item ID. Claude Code replaces dots with hyphens in directory names,
// so wi-266d.2 becomes wi-266d-2 on disk. This reverses that: find the last hyphen
// followed by only digits and replace it with a dot.
//
// Only converts if there are at least 2 hyphens, so that goal IDs like "wi-a3f8"
// or "wi-1234" are not mistakenly converted to "wi.a3f8" or "wi.1234".
func taskListDirToWorkItemID(dirName string) string {
	lastHyphen := strings.LastIndex(dirName, "-")
	if lastHyphen < 0 || lastHyphen == len(dirName)-1 {
		return dirName
	}

	// Count hyphens: need at least 2 (the "wi-" prefix + child separator).
	// A goal like "wi-a3f8" has only 1 hyphen and should stay unchanged.
	if strings.Count(dirName, "-") < 2 {
		return dirName
	}

	suffix := dirName[lastHyphen+1:]
	allDigits := true
	for _, r := range suffix {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}

	if allDigits {
		return dirName[:lastHyphen] + "." + suffix
	}
	return dirName
}
