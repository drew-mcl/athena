package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/executil"
	"github.com/drewfead/athena/internal/store"
	"github.com/google/uuid"
)

// registerMergeQueueHandlers registers all merge queue API handlers.
func (d *Daemon) registerMergeQueueHandlers() {
	d.server.Handle("get_merge_queue", d.handleGetMergeQueue)
	d.server.Handle("get_merge_queue_head", d.handleGetMergeQueueHead)
	d.server.Handle("add_to_merge_queue", d.handleAddToMergeQueue)
	d.server.Handle("remove_from_merge_queue", d.handleRemoveFromMergeQueue)
	d.server.Handle("bump_merge_queue_item", d.handleBumpMergeQueueItem)
	d.server.Handle("rebase_merge_queue_item", d.handleRebaseMergeQueueItem)
	d.server.Handle("reconcile_queue", d.handleReconcileQueue)
}

func (d *Daemon) handleGetMergeQueue(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if err := d.refreshQueueGraph(req.Project); err != nil {
		return nil, err
	}

	items, err := d.store.GetMergeQueue(req.Project)
	if err != nil {
		return nil, err
	}

	// Convert to API types
	result := make([]*control.MergeQueueItemInfo, len(items))
	for i, item := range items {
		result[i] = mergeQueueItemToInfo(item)
	}
	return result, nil
}

func (d *Daemon) handleGetMergeQueueHead(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	branch, commit, err := d.getIntegrationHead(req.Project)
	if err != nil {
		return nil, err
	}

	return &control.MergeQueueHeadInfo{
		Branch: branch,
		Commit: commit,
		Empty:  branch == "",
	}, nil
}

func (d *Daemon) handleAddToMergeQueue(params json.RawMessage) (any, error) {
	var req control.AddToMergeQueueRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Get worktree info
	wt, err := d.store.GetWorktree(req.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}
	if wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", req.WorktreePath)
	}

	// Use provided project or get from worktree
	project := req.Project
	if project == "" {
		if wt.ProjectName != nil {
			project = *wt.ProjectName
		} else {
			project = wt.Project
		}
	}

	// Get current HEAD commit of the worktree
	headCommit, err := getGitHead(req.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	// Determine the actual base branch and commit by finding the merge-base.
	// This correctly detects what the worktree was forked from (usually main).
	baseBranch, baseCommit, err := getGitMergeBase(req.WorktreePath, wt.Branch)
	if err != nil {
		return nil, fmt.Errorf("failed to determine base commit: %w", err)
	}

	if baseBranch == "" || baseCommit == "" {
		return nil, fmt.Errorf("failed to determine queue base (branch=%q commit=%q)", baseBranch, baseCommit)
	}

	item := &store.MergeQueueItem{
		ID:           uuid.NewString()[:8],
		Project:      project,
		WorktreePath: req.WorktreePath,
		Branch:       wt.Branch,
		BaseBranch:   baseBranch,
		BaseCommit:   baseCommit,
		HeadCommit:   headCommit,
	}

	if err := d.store.AddToMergeQueue(item); err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type: "merge_queue_updated",
		Payload: map[string]string{
			"project": project,
			"action":  "added",
			"path":    req.WorktreePath,
		},
	})

	return mergeQueueItemToInfo(item), nil
}

func (d *Daemon) handleRemoveFromMergeQueue(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Get item first to know project for broadcast
	item, err := d.store.GetMergeQueueItem(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	project := ""
	if item != nil {
		project = item.Project
	}

	if err := d.store.RemoveFromMergeQueue(req.WorktreePath); err != nil {
		return nil, err
	}

	if project != "" {
		d.server.Broadcast(control.Event{
			Type: "merge_queue_updated",
			Payload: map[string]string{
				"project": project,
				"action":  "removed",
				"path":    req.WorktreePath,
			},
		})
	}

	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleBumpMergeQueueItem(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Get current HEAD as the new base commit
	headCommit, err := getGitHead(req.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	// Get the item before moving to know its original position
	originalItem, err := d.store.GetMergeQueueItem(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	if originalItem == nil {
		return nil, fmt.Errorf("worktree not in queue: %s", req.WorktreePath)
	}

	if err := d.store.MoveToBackOfQueue(req.WorktreePath, headCommit); err != nil {
		return nil, err
	}

	// Auto-rebase dependent features that were marked as diverged/rebasing.
	rebaseResults := d.cascadeRebase(originalItem.Project)

	// Get updated item
	item, err := d.store.GetMergeQueueItem(req.WorktreePath)
	if err != nil {
		return nil, err
	}

	d.server.Broadcast(control.Event{
		Type: "merge_queue_updated",
		Payload: map[string]any{
			"project":        item.Project,
			"action":         "bumped",
			"path":           req.WorktreePath,
			"rebase_results": rebaseResults,
		},
	})

	return mergeQueueItemToInfo(item), nil
}

func (d *Daemon) handleReconcileQueue(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.refreshQueueGraph(req.Project); err != nil {
		return nil, fmt.Errorf("failed to refresh queue graph: %w", err)
	}

	rebaseResults := d.cascadeRebase(req.Project)

	d.server.Broadcast(control.Event{
		Type: "merge_queue_updated",
		Payload: map[string]any{
			"project":        req.Project,
			"action":         "reconciled",
			"rebase_results": rebaseResults,
		},
	})

	return &control.ReconcileQueueResult{Results: rebaseResults}, nil
}

// cascadeRebase automatically rebases items marked as rebasing/diverged in the queue.
// Returns a summary of rebase results.
func (d *Daemon) cascadeRebase(project string) []map[string]string {
	var results []map[string]string

	items, err := d.store.GetItemsNeedingRebase(project)
	if err != nil || len(items) == 0 {
		return results
	}

	// Get the full queue to determine rebase targets
	queue, err := d.store.GetMergeQueue(project)
	if err != nil {
		return results
	}

	// Build a map of position -> item for easy lookup
	positionMap := make(map[int]*store.MergeQueueItem)
	for _, item := range queue {
		positionMap[item.Position] = item
	}

	for _, item := range items {
		queueItem := item
		if mapped, ok := positionMap[item.Position]; ok && mapped != nil {
			queueItem = mapped
		} else {
			positionMap[item.Position] = queueItem
		}

		result := map[string]string{
			"path":   queueItem.WorktreePath,
			"branch": queueItem.Branch,
		}

		// Resolve the exact target reference and commit to preserve queue chain semantics.
		rebaseOnto, newBaseCommit, newBaseBranch, err := resolveQueueRebaseTarget(queueItem, positionMap)
		if err != nil {
			result["status"] = "error"
			result["error"] = err.Error()
			if updateErr := d.store.UpdateMergeQueueItem(queueItem.WorktreePath, store.MergeQueueStatusConflict, ""); updateErr != nil {
				result["update_error"] = updateErr.Error()
			}
			queueItem.Status = store.MergeQueueStatusConflict
			results = append(results, result)
			continue
		}
		result["base"] = newBaseBranch
		result["onto"] = rebaseOnto

		// Perform the rebase
		err = gitRebase(queueItem.WorktreePath, rebaseOnto)
		if err != nil {
			result["status"] = "conflict"
			result["error"] = err.Error()

			// Mark as conflict in store
			if updateErr := d.store.UpdateMergeQueueItem(queueItem.WorktreePath, store.MergeQueueStatusConflict, ""); updateErr != nil {
				result["update_error"] = updateErr.Error()
			}
			queueItem.Status = store.MergeQueueStatusConflict
		} else {
			// Get new HEAD after rebase
			newHead, headErr := getGitHead(queueItem.WorktreePath)
			if headErr != nil {
				result["status"] = "error"
				result["error"] = fmt.Sprintf("rebase succeeded but failed to resolve new HEAD: %v", headErr)
				if updateErr := d.store.UpdateMergeQueueItem(queueItem.WorktreePath, store.MergeQueueStatusConflict, ""); updateErr != nil {
					result["update_error"] = updateErr.Error()
				}
				queueItem.Status = store.MergeQueueStatusConflict
				results = append(results, result)
				continue
			}

			// Always move in-memory queue state forward so downstream rebases target the new integration head.
			queueItem.Status = store.MergeQueueStatusQueued
			queueItem.BaseBranch = newBaseBranch
			queueItem.BaseCommit = newBaseCommit
			queueItem.HeadCommit = newHead

			// Mark as rebased in store
			if updateErr := d.store.MarkQueueItemRebasedWithBase(queueItem.WorktreePath, newBaseBranch, newBaseCommit, newHead); updateErr != nil {
				result["status"] = "error"
				result["error"] = fmt.Sprintf("rebase succeeded but failed to persist queue state: %v", updateErr)
			} else {
				result["status"] = "success"
				result["head"] = newHead
			}
		}

		results = append(results, result)
	}

	return results
}

// getIntegrationHead returns the current integration base for spawning new worktrees.
func (d *Daemon) getIntegrationHead(project string) (string, string, error) {
	if err := d.refreshQueueGraph(project); err != nil {
		return "", "", err
	}
	return d.store.GetQueueHead(project)
}

// refreshQueueGraph updates queue head commits from git and marks downstream divergence.
func (d *Daemon) refreshQueueGraph(project string) error {
	items, err := d.store.GetMergeQueue(project)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	// First pass: refresh each node's current HEAD commit.
	for _, item := range items {
		currentHead, err := getGitHead(item.WorktreePath)
		if err != nil {
			continue
		}
		if currentHead == item.HeadCommit {
			continue
		}
		if err := d.store.UpdateMergeQueueItem(item.WorktreePath, item.Status, currentHead); err != nil {
			return err
		}
		item.HeadCommit = currentHead
	}

	// Head of queue is always the root of the integration chain.
	if items[0].Status == store.MergeQueueStatusDiverged {
		if err := d.store.UpdateMergeQueueItem(items[0].WorktreePath, store.MergeQueueStatusQueued, items[0].HeadCommit); err != nil {
			return err
		}
		items[0].Status = store.MergeQueueStatusQueued
	}

	stableHead := items[0].HeadCommit
	for i := 1; i < len(items); i++ {
		item := items[i]

		// If base commit no longer matches upstream HEAD, this and descendants diverged.
		if stableHead != "" && item.BaseCommit != stableHead {
			return d.store.MarkQueueItemsDiverged(project, item.Position)
		}

		// If this item was previously diverged and now lines up, clear it.
		if item.Status == store.MergeQueueStatusDiverged {
			if err := d.store.UpdateMergeQueueItem(item.WorktreePath, store.MergeQueueStatusQueued, item.HeadCommit); err != nil {
				return err
			}
		}

		stableHead = item.HeadCommit
	}

	return nil
}

func (d *Daemon) handleRebaseMergeQueueItem(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath  string `json:"worktree_path"`
		NewBaseCommit string `json:"new_base_commit"`
		NewHeadCommit string `json:"new_head_commit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.MarkQueueItemRebased(req.WorktreePath, req.NewBaseCommit, req.NewHeadCommit); err != nil {
		return nil, err
	}

	item, err := d.store.GetMergeQueueItem(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	if item != nil {
		d.server.Broadcast(control.Event{
			Type: "merge_queue_updated",
			Payload: map[string]string{
				"project": item.Project,
				"action":  "rebased",
				"path":    req.WorktreePath,
			},
		})
	}

	return map[string]bool{"success": true}, nil
}

// Helper functions

func mergeQueueItemToInfo(item *store.MergeQueueItem) *control.MergeQueueItemInfo {
	return &control.MergeQueueItemInfo{
		ID:           item.ID,
		Project:      item.Project,
		WorktreePath: item.WorktreePath,
		Branch:       item.Branch,
		Position:     item.Position,
		Status:       string(item.Status),
		BaseBranch:   item.BaseBranch,
		BaseCommit:   item.BaseCommit,
		HeadCommit:   item.HeadCommit,
		CreatedAt:    item.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:    item.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func getGitHead(path string) (string, error) {
	cmd, err := executil.Command("git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getGitMergeBase(path, branch string) (baseBranch string, baseCommit string, err error) {
	baseRef, baseBranch, err := getGitDefaultBaseRef(path)
	if err != nil {
		return "", "", err
	}

	cmd, err := executil.Command("git", "merge-base", baseRef, branch)
	if err != nil {
		return "", "", err
	}
	cmd.Dir = path
	out, err := cmd.Output()
	if err == nil {
		return baseBranch, strings.TrimSpace(string(out)), nil
	}

	return "", "", fmt.Errorf("could not find merge base between %s and %s", baseRef, branch)
}

// gitRebase performs a git rebase onto the specified branch.
// Returns nil on success, error on conflict or failure.
func gitRebase(worktreePath, ontoBranch string) error {
	cmd, err := executil.Command("git", "rebase", ontoBranch)
	if err != nil {
		return fmt.Errorf("rebase failed: %w", err)
	}
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a conflict
		if strings.Contains(string(output), "CONFLICT") || strings.Contains(string(output), "could not apply") {
			// Abort the rebase to leave clean state
			if abortCmd, abortErr := executil.Command("git", "rebase", "--abort"); abortErr == nil {
				abortCmd.Dir = worktreePath
				abortCmd.Run() // Ignore error from abort
			}

			return fmt.Errorf("rebase conflict: %s", strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("rebase failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func resolveQueueRebaseTarget(item *store.MergeQueueItem, positionMap map[int]*store.MergeQueueItem) (rebaseRef, baseCommit, baseBranch string, err error) {
	if item == nil {
		return "", "", "", fmt.Errorf("queue item is required")
	}

	if rebaseRef, baseCommit, baseBranch, ok := selectQueueRebaseTarget(item, positionMap); ok {
		if baseCommit == "" {
			baseCommit, err = getGitRevParse(item.WorktreePath, rebaseRef)
			if err != nil {
				return "", "", "", fmt.Errorf("failed to resolve rebase target %q: %w", rebaseRef, err)
			}
		}
		return rebaseRef, baseCommit, baseBranch, nil
	}

	if item.Position > 1 {
		return "", "", "", fmt.Errorf("queue item at position %d has no stable predecessor", item.Position)
	}

	rebaseRef, baseBranch, err = getGitDefaultBaseRef(item.WorktreePath)
	if err != nil {
		return "", "", "", err
	}
	baseCommit, err = getGitRevParse(item.WorktreePath, rebaseRef)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to resolve default base ref %q: %w", rebaseRef, err)
	}
	return rebaseRef, baseCommit, baseBranch, nil
}

func selectQueueRebaseTarget(item *store.MergeQueueItem, positionMap map[int]*store.MergeQueueItem) (rebaseRef, baseCommit, baseBranch string, ok bool) {
	if item == nil || item.Position <= 1 {
		return "", "", "", false
	}
	prevItem, exists := positionMap[item.Position-1]
	if !exists || prevItem == nil {
		return "", "", "", false
	}
	if !isQueueItemStable(prevItem) {
		return "", "", "", false
	}

	if prevItem.HeadCommit != "" {
		return prevItem.HeadCommit, prevItem.HeadCommit, prevItem.Branch, true
	}
	if prevItem.Branch != "" {
		return prevItem.Branch, "", prevItem.Branch, true
	}
	return "", "", "", false
}

func isQueueItemStable(item *store.MergeQueueItem) bool {
	if item == nil {
		return false
	}
	switch item.Status {
	case "", store.MergeQueueStatusQueued, store.MergeQueueStatusMerging, store.MergeQueueStatusMerged:
		return true
	default:
		return false
	}
}

func getGitDefaultBaseRef(path string) (ref string, branch string, err error) {
	originHead, err := gitOutput(path, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil && originHead != "" {
		return originHead, strings.TrimPrefix(originHead, "origin/"), nil
	}

	for _, candidate := range []string{"main", "master"} {
		if _, err := gitOutput(path, "rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate, candidate, nil
		}
		originCandidate := "origin/" + candidate
		if _, err := gitOutput(path, "rev-parse", "--verify", "--quiet", originCandidate); err == nil {
			return originCandidate, candidate, nil
		}
	}

	return "", "", fmt.Errorf("could not determine default base branch")
}

func getGitRevParse(path, ref string) (string, error) {
	return gitOutput(path, "rev-parse", ref)
}

func gitOutput(path string, args ...string) (string, error) {
	cmd, err := executil.Command("git", args...)
	if err != nil {
		return "", err
	}
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
