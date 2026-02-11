package daemon

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

// registerHookHandlers registers all Claude Code lifecycle hook API handlers.
func (d *Daemon) registerHookHandlers() {
	d.server.Handle("hook_session_start", d.handleHookSessionStart)
	d.server.Handle("hook_stop", d.handleHookStop)
	d.server.Handle("hook_session_end", d.handleHookSessionEnd)
}

// handleHookSessionStart handles the session-start lifecycle event.
// - Looks up the work item
// - If feature with worktree not in queue → auto-add to merge queue
// - If work item status is pending → mark in_progress
// - Broadcasts event for TUI
func (d *Daemon) handleHookSessionStart(params json.RawMessage) (any, error) {
	var req control.HookSessionStartRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if req.WorkItemID == "" {
		return &control.HookEventResponse{Success: true, Message: "no work item"}, nil
	}

	item, err := d.store.GetWorkItem(req.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item: %w", err)
	}
	if item == nil {
		return &control.HookEventResponse{Success: true, Message: "work item not found"}, nil
	}

	var actions []string

	// Auto-add feature worktree to merge queue
	if item.ItemType == store.WorkItemTypeFeature && item.WorktreePath != nil {
		if err := d.autoAddToQueue(item); err != nil {
			logging.Warn("hook: failed to auto-add to queue", "work_item", req.WorkItemID, "error", err)
		} else {
			actions = append(actions, "queued")
		}
	}

	// Mark pending → in_progress
	if item.Status == store.WorkItemStatusPending {
		if err := d.store.UpdateWorkItemStatus(req.WorkItemID, store.WorkItemStatusInProgress); err != nil {
			logging.Warn("hook: failed to update work item status", "work_item", req.WorkItemID, "error", err)
		} else {
			actions = append(actions, "in_progress")
		}
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type: "hook_session_start",
		Payload: map[string]string{
			"work_item_id": req.WorkItemID,
			"actions":      strings.Join(actions, ","),
		},
	})

	msg := "ok"
	if len(actions) > 0 {
		msg = strings.Join(actions, ", ")
	}
	return &control.HookEventResponse{Success: true, Message: msg}, nil
}

// handleHookStop handles the stop lifecycle event.
// - Checks if a PR exists for the worktree
// - Updates queue and work item status accordingly
func (d *Daemon) handleHookStop(params json.RawMessage) (any, error) {
	var req control.HookStopRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if req.WorkItemID == "" {
		return &control.HookEventResponse{Success: true, Message: "no work item"}, nil
	}

	actions, err := d.checkFeatureCompletion(req.WorkItemID, req.WorkDir)
	if err != nil {
		logging.Warn("hook: completion check failed", "work_item", req.WorkItemID, "error", err)
	}

	d.server.Broadcast(control.Event{
		Type: "hook_stop",
		Payload: map[string]string{
			"work_item_id": req.WorkItemID,
			"actions":      strings.Join(actions, ","),
		},
	})

	msg := "ok"
	if len(actions) > 0 {
		msg = strings.Join(actions, ", ")
	}
	return &control.HookEventResponse{Success: true, Message: msg}, nil
}

// handleHookSessionEnd handles the session-end lifecycle event.
// Same logic as stop — check PR status and update accordingly.
func (d *Daemon) handleHookSessionEnd(params json.RawMessage) (any, error) {
	var req control.HookSessionEndRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if req.WorkItemID == "" {
		return &control.HookEventResponse{Success: true, Message: "no work item"}, nil
	}

	actions, err := d.checkFeatureCompletion(req.WorkItemID, req.WorkDir)
	if err != nil {
		logging.Warn("hook: completion check failed", "work_item", req.WorkItemID, "error", err)
	}

	d.server.Broadcast(control.Event{
		Type: "hook_session_end",
		Payload: map[string]string{
			"work_item_id": req.WorkItemID,
			"actions":      strings.Join(actions, ","),
		},
	})

	msg := "ok"
	if len(actions) > 0 {
		msg = strings.Join(actions, ", ")
	}
	return &control.HookEventResponse{Success: true, Message: msg}, nil
}

// autoAddToQueue adds a feature worktree to the merge queue if not already present.
func (d *Daemon) autoAddToQueue(item *store.WorkItem) error {
	if item.WorktreePath == nil {
		return nil
	}

	// Check if already in queue
	existing, err := d.store.GetMergeQueueItem(*item.WorktreePath)
	if err != nil {
		return fmt.Errorf("check queue: %w", err)
	}
	if existing != nil {
		return nil // Already queued
	}

	// Get worktree details
	wt, err := d.store.GetWorktree(*item.WorktreePath)
	if err != nil || wt == nil {
		return fmt.Errorf("worktree not found: %s", *item.WorktreePath)
	}

	// Resolve project
	project := item.Project
	if project == "" {
		if wt.ProjectName != nil {
			project = *wt.ProjectName
		} else {
			project = wt.Project
		}
	}

	// Get HEAD commit
	headCommit, err := getGitHead(*item.WorktreePath)
	if err != nil {
		return fmt.Errorf("get HEAD: %w", err)
	}

	// Determine base
	baseBranch, baseCommit := "", ""
	if qBranch, qCommit, qErr := d.getIntegrationHead(project); qErr == nil && qBranch != "" && qCommit != "" {
		baseBranch = qBranch
		baseCommit = qCommit
	} else {
		baseBranch, baseCommit, err = getGitMergeBase(*item.WorktreePath, wt.Branch)
		if err != nil {
			return fmt.Errorf("merge base: %w", err)
		}
	}

	queueItem := &store.MergeQueueItem{
		ID:           shortID(),
		Project:      project,
		WorktreePath: *item.WorktreePath,
		Branch:       wt.Branch,
		BaseBranch:   baseBranch,
		BaseCommit:   baseCommit,
		HeadCommit:   headCommit,
	}

	if err := d.store.AddToMergeQueue(queueItem); err != nil {
		return fmt.Errorf("add to queue: %w", err)
	}

	logging.Info("hook: auto-added to merge queue",
		"work_item", item.ID,
		"worktree", *item.WorktreePath,
		"branch", wt.Branch)

	d.server.Broadcast(control.Event{
		Type: "merge_queue_updated",
		Payload: map[string]string{
			"project": project,
			"action":  "added",
			"path":    *item.WorktreePath,
			"source":  "hook",
		},
	})

	return nil
}

// checkFeatureCompletion checks PR status and updates work item/queue accordingly.
func (d *Daemon) checkFeatureCompletion(workItemID, workDir string) ([]string, error) {
	var actions []string

	item, err := d.store.GetWorkItem(workItemID)
	if err != nil || item == nil {
		return actions, fmt.Errorf("work item not found: %s", workItemID)
	}

	// Only check features with worktrees
	if item.ItemType != store.WorkItemTypeFeature || item.WorktreePath == nil {
		return actions, nil
	}

	// Use workDir or worktree path for PR check
	checkDir := workDir
	if checkDir == "" && item.WorktreePath != nil {
		checkDir = *item.WorktreePath
	}
	if checkDir == "" {
		return actions, nil
	}

	// Check PR status via gh CLI
	prState, prURL := checkPRStatus(checkDir)

	// Update PR URL on work item if found
	if prURL != "" && (item.PRURL == nil || *item.PRURL != prURL) {
		d.store.UpdateWorkItemPRURL(workItemID, prURL)
		actions = append(actions, "pr_found")
	}

	// If PR is merged, update everything
	if prState == "MERGED" {
		// Update queue item
		if item.WorktreePath != nil {
			if qItem, err := d.store.GetMergeQueueItem(*item.WorktreePath); err == nil && qItem != nil {
				d.store.UpdateMergeQueueItem(*item.WorktreePath, store.MergeQueueStatusMerged, qItem.HeadCommit)
				actions = append(actions, "queue_merged")
			}
		}

		// Mark work item completed
		if item.Status != store.WorkItemStatusCompleted {
			d.store.UpdateWorkItemStatus(workItemID, store.WorkItemStatusCompleted)
			actions = append(actions, "completed")
		}
	}

	return actions, nil
}

// checkPRStatus uses gh CLI to check if a PR exists and its state.
// Returns (state, url) where state is "OPEN", "MERGED", "CLOSED", or "".
func checkPRStatus(workDir string) (state, url string) {
	cmd := exec.Command("gh", "pr", "view", "--json", "state,url", "--jq", ".state + \" \" + .url")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), " ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return "", ""
}

// shortID generates a short random ID for queue items.
func shortID() string {
	return generateID()[:8]
}
