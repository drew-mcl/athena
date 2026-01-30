package daemon

import (
	"context"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/plugin"
	"github.com/drewfead/athena/internal/plugin/vcs"
	"github.com/drewfead/athena/internal/store"
)

// QueueSync monitors PR status and syncs the merge queue.
// Works locally for queue management; VCS plugins add PR/CI awareness.
type QueueSync struct {
	store    *store.Store
	plugins  *plugin.Registry
	server   *control.Server
	interval time.Duration
	stopCh   chan struct{}
}

// NewQueueSync creates a new queue sync monitor.
func NewQueueSync(store *store.Store, plugins *plugin.Registry, server *control.Server) *QueueSync {
	return &QueueSync{
		store:    store,
		plugins:  plugins,
		server:   server,
		interval: 30 * time.Second, // Poll every 30 seconds
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background sync loop.
func (q *QueueSync) Start() {
	go q.loop()
}

// Stop halts the sync loop.
func (q *QueueSync) Stop() {
	close(q.stopCh)
}

func (q *QueueSync) loop() {
	ticker := time.NewTicker(q.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			q.syncAllQueues()
		case <-q.stopCh:
			return
		}
	}
}

// syncAllQueues checks all queue items for merged PRs.
func (q *QueueSync) syncAllQueues() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Get all queued items with PR URLs
	// For now, get all projects and check their queues
	projects := q.getActiveProjects()

	for _, project := range projects {
		q.syncProject(ctx, project)
	}
}

func (q *QueueSync) syncProject(ctx context.Context, project string) {
	items, err := q.store.GetMergeQueue(project)
	if err != nil {
		return
	}

	// Get enabled VCS plugins
	vcsPlugins := q.plugins.GetByCategory(plugin.CategoryVCS)
	if len(vcsPlugins) == 0 {
		// No VCS plugins enabled - queue works locally but no PR sync
		return
	}

	// Use the first enabled VCS plugin (could be smarter about detection later)
	var vcsProvider vcs.Provider
	for _, p := range vcsPlugins {
		if v, ok := p.(vcs.Provider); ok {
			vcsProvider = v
			break
		}
	}
	if vcsProvider == nil {
		return
	}

	for _, item := range items {
		// Skip items without a PR URL
		wt, _ := q.store.GetWorktree(item.WorktreePath)
		if wt == nil || wt.PRURL == nil || *wt.PRURL == "" {
			continue
		}

		// Check PR status
		pr, err := vcsProvider.GetPR(ctx, project, item.Branch)
		if err != nil {
			logging.Debug("failed to get PR status", "branch", item.Branch, "error", err)
			continue
		}

		switch pr.State {
		case vcs.PRStateMerged:
			// PR merged! Remove from queue
			logging.Info("PR merged, removing from queue", "branch", item.Branch, "pr", pr.Number)

			if err := q.store.RemoveFromMergeQueue(item.WorktreePath); err != nil {
				logging.Error("failed to remove merged item from queue", "error", err)
				continue
			}

			// Update worktree status
			q.store.UpdateWorktreeStatus(item.WorktreePath, store.WorktreeStatusMerged)

			// Broadcast event
			q.server.Broadcast(control.Event{
				Type: "merge_queue_updated",
				Payload: map[string]any{
					"project": project,
					"action":  "merged",
					"path":    item.WorktreePath,
					"branch":  item.Branch,
					"pr":      pr.Number,
				},
			})

		case vcs.PRStateClosed:
			// PR closed without merge - just update status, don't remove
			logging.Info("PR closed without merge", "branch", item.Branch)
			q.store.UpdateMergeQueueItem(item.WorktreePath, store.MergeQueueStatusConflict, "")
		}
	}
}

// getActiveProjects returns a list of projects that have items in the queue.
func (q *QueueSync) getActiveProjects() []string {
	projects, err := q.store.GetActiveQueueProjects()
	if err != nil {
		logging.Debug("failed to get active queue projects", "error", err)
		return nil
	}
	return projects
}
