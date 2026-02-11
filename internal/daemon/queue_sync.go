package daemon

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/plugin"
	"github.com/drewfead/athena/internal/plugin/vcs"
	"github.com/drewfead/athena/internal/store"
)

// QueueSync monitors PR status and syncs the merge queue.
// Works locally for queue management; VCS plugins add PR/CI awareness.
// When auto-merge is enabled, it merges position-1 PRs that pass CI.
type QueueSync struct {
	store    *store.Store
	plugins  *plugin.Registry
	server   *control.Server
	config   *config.Config
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once

	// onAutoMerged is called after a successful auto-merge to cascade rebase remaining items.
	onAutoMerged func(project string)
}

// NewQueueSync creates a new queue sync monitor.
func NewQueueSync(store *store.Store, plugins *plugin.Registry, server *control.Server, cfg *config.Config) *QueueSync {
	return &QueueSync{
		store:    store,
		plugins:  plugins,
		server:   server,
		config:   cfg,
		interval: 30 * time.Second, // Poll every 30 seconds
		stopCh:   make(chan struct{}),
	}
}

// SetAutoMergedCallback registers a function called after auto-merge succeeds.
func (q *QueueSync) SetAutoMergedCallback(fn func(project string)) {
	q.onAutoMerged = fn
}

// Start begins the background sync loop.
func (q *QueueSync) Start() {
	go q.loop()
}

// Stop halts the sync loop.
func (q *QueueSync) Stop() {
	q.stopOnce.Do(func() {
		close(q.stopCh)
	})
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

	q.refreshPluginConfig()

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

	if q.plugins == nil {
		return
	}

	// Get enabled VCS plugins
	vcsPlugins := q.plugins.GetEnabledByCategory(plugin.CategoryVCS)
	if len(vcsPlugins) == 0 {
		// No VCS plugins enabled - queue works locally but no PR sync
		return
	}

	autoMergeEnabled := q.config != nil && q.config.Integrations.GitHub.AutoMerge
	mergeMethod := vcs.MergeMethodRebase
	if q.config != nil && q.config.Integrations.GitHub.MergeMethod != "" {
		mergeMethod = vcs.MergeMethod(q.config.Integrations.GitHub.MergeMethod)
	}

	for _, item := range items {
		// Skip items without a PR URL
		wt, err := q.store.GetWorktree(item.WorktreePath)
		if err != nil {
			logging.Debug("failed to load worktree for queue item", "path", item.WorktreePath, "error", err)
			continue
		}
		if wt == nil || wt.PRURL == nil || *wt.PRURL == "" {
			continue
		}

		vcsProvider, repo, ok := resolveVCSProviderAndRepo(project, *wt.PRURL, vcsPlugins)
		if !ok {
			logging.Debug("failed to resolve VCS provider from PR URL",
				"project", project,
				"branch", item.Branch,
				"pr_url", *wt.PRURL,
			)
			continue
		}

		// Check PR status
		pr, err := vcsProvider.GetPR(ctx, repo, item.Branch)
		if err != nil {
			logging.Debug("failed to get PR status", "repo", repo, "branch", item.Branch, "error", err)
			continue
		}

		switch pr.State {
		case vcs.PRStateMerged:
			// PR merged! Remove from queue
			logging.Info("PR merged, removing from queue", "branch", item.Branch, "pr", pr.Number)
			q.handleMergedPR(project, item, pr)

		case vcs.PRStateClosed:
			// PR closed without merge - just update status, don't remove
			logging.Info("PR closed without merge", "branch", item.Branch)
			if err := q.store.UpdateMergeQueueItem(item.WorktreePath, store.MergeQueueStatusConflict, ""); err != nil {
				logging.Error("failed to mark queue item as conflict", "path", item.WorktreePath, "error", err)
			}

		case vcs.PRStateOpen:
			// Auto-merge: only position 1, only queued status, only if enabled
			if !autoMergeEnabled || item.Position != 1 || item.Status != store.MergeQueueStatusQueued {
				continue
			}
			q.tryAutoMerge(ctx, project, item, vcsProvider, repo, mergeMethod)
		}
	}
}

// handleMergedPR processes a PR that was merged (externally or via auto-merge).
func (q *QueueSync) handleMergedPR(project string, item *store.MergeQueueItem, pr *vcs.PullRequest) {
	if err := q.store.RemoveFromMergeQueue(item.WorktreePath); err != nil {
		logging.Error("failed to remove merged item from queue", "error", err)
		return
	}

	if err := q.store.UpdateWorktreeStatus(item.WorktreePath, store.WorktreeStatusMerged); err != nil {
		logging.Error("failed to update merged worktree status", "path", item.WorktreePath, "error", err)
	}

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

	// After merge, remaining items need to rebase against updated main.
	if q.onAutoMerged != nil {
		q.onAutoMerged(project)
	}
}

// tryAutoMerge checks if a position-1 PR is ready to merge and merges it.
func (q *QueueSync) tryAutoMerge(ctx context.Context, project string, item *store.MergeQueueItem, provider vcs.Provider, repo string, method vcs.MergeMethod) {
	readiness, err := provider.GetMergeReadiness(ctx, repo, item.Branch)
	if err != nil {
		logging.Debug("failed to check merge readiness", "branch", item.Branch, "error", err)
		return
	}

	if !readiness.Ready {
		logging.Debug("PR not ready for auto-merge", "branch", item.Branch, "reason", readiness.Reason)
		return
	}

	// Mark as merging
	if err := q.store.UpdateMergeQueueItem(item.WorktreePath, store.MergeQueueStatusMerging, ""); err != nil {
		logging.Error("failed to mark queue item as merging", "path", item.WorktreePath, "error", err)
		return
	}

	logging.Info("auto-merging PR", "branch", item.Branch, "method", string(method))

	if err := provider.MergePR(ctx, repo, item.Branch, method); err != nil {
		logging.Error("auto-merge failed", "branch", item.Branch, "error", err)
		// Revert to queued so it can be retried next cycle
		if updateErr := q.store.UpdateMergeQueueItem(item.WorktreePath, store.MergeQueueStatusQueued, ""); updateErr != nil {
			logging.Error("failed to revert queue item status after merge failure", "path", item.WorktreePath, "error", updateErr)
		}
		return
	}

	logging.Info("auto-merge succeeded", "branch", item.Branch)

	// Fetch PR details for the merged event
	pr, err := provider.GetPR(ctx, repo, item.Branch)
	if err != nil {
		// Merge succeeded but we can't get PR details — still handle it
		pr = &vcs.PullRequest{Branch: item.Branch}
	}

	q.handleMergedPR(project, item, pr)
}

func (q *QueueSync) refreshPluginConfig() {
	// Re-read plugin enablement each sync cycle so CLI toggles take effect without daemon restart.
	if err := plugin.RefreshRegistryFromDisk(q.plugins); err != nil {
		logging.Debug("failed to refresh plugin config in queue sync", "error", err)
	}
}

func resolveVCSProviderAndRepo(project, prURL string, vcsPlugins []plugin.Plugin) (vcs.Provider, string, bool) {
	if providerName, repo, ok := parseRepoFromPRURL(prURL); ok {
		if provider := findVCSProvider(vcsPlugins, providerName); provider != nil {
			return provider, repo, true
		}
		return nil, "", false
	}

	// Fallback for non-standard/self-hosted PR URLs: use first enabled provider and project name.
	for _, p := range vcsPlugins {
		if provider, ok := p.(vcs.Provider); ok {
			return provider, project, true
		}
	}

	return nil, "", false
}

func findVCSProvider(vcsPlugins []plugin.Plugin, providerName string) vcs.Provider {
	for _, p := range vcsPlugins {
		if !strings.EqualFold(p.Name(), providerName) {
			continue
		}
		if provider, ok := p.(vcs.Provider); ok {
			return provider
		}
	}
	return nil
}

func parseRepoFromPRURL(prURL string) (providerName, repo string, ok bool) {
	u, err := url.Parse(prURL)
	if err != nil {
		return "", "", false
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 {
		return "", "", false
	}

	host := strings.ToLower(u.Hostname())
	switch {
	case strings.Contains(host, "github"):
		if len(segments) < 4 || segments[2] != "pull" {
			return "", "", false
		}
		return "github", segments[0] + "/" + segments[1], true
	case strings.Contains(host, "gitlab"):
		// GitLab merge request URLs look like:
		// /group/subgroup/repo/-/merge_requests/123
		dashIdx := -1
		for i, segment := range segments {
			if segment == "-" {
				dashIdx = i
				break
			}
		}
		if dashIdx < 2 || dashIdx+1 >= len(segments) || segments[dashIdx+1] != "merge_requests" {
			return "", "", false
		}
		return "gitlab", strings.Join(segments[:dashIdx], "/"), true
	default:
		return "", "", false
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
