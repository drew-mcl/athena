// Package daemon implements the athenad background service.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/config"
	actx "github.com/drewfead/athena/internal/context"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/task"
	"github.com/drewfead/athena/internal/task/claude"
	"github.com/drewfead/athena/internal/worktree"
	"gopkg.in/yaml.v3"
)

// ShutdownTimeout is how long to wait for graceful shutdown.
const ShutdownTimeout = 30 * time.Second

// DrainTimeout is how long to wait for in-flight jobs to complete.
const DrainTimeout = 60 * time.Second

// Daemon is the main orchestrator service.
type Daemon struct {
	config      *config.Config
	store       *store.Store
	server      *control.Server
	scanner     *worktree.Scanner
	provisioner *worktree.Provisioner
	migrator    *worktree.Migrator
	publisher   *worktree.Publisher
	spawner     *agent.Spawner
	executor    *JobExecutor

	// Task management (Claude Code tasks integration)
	taskRegistry *task.Registry

	agents   map[string]*AgentProcess
	agentsMu sync.RWMutex

	// Job queue for execution
	jobQueue chan string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Shutdown coordination
	shutdownOnce sync.Once
	draining     bool
	drainingMu   sync.RWMutex
}

// AgentProcess represents a running Claude Code process.
type AgentProcess struct {
	ID     string
	PID    int
	Done   chan struct{}
	Cancel context.CancelFunc
}

// New creates a new daemon instance.
func New(cfg *config.Config) (*Daemon, error) {
	st, err := store.New(cfg.Daemon.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	publisher := worktree.NewPublisher(cfg, st)

	// Initialize task registry with Claude Code provider
	taskRegistry := task.NewRegistry()
	claudeProvider, err := claude.NewProvider()
	if err != nil {
		logging.Warn("failed to initialize Claude task provider", "error", err)
	} else {
		if err := taskRegistry.Register(claudeProvider); err != nil {
			logging.Warn("failed to register Claude task provider", "error", err)
		}
	}

	d := &Daemon{
		config:       cfg,
		store:        st,
		server:       control.NewServer(cfg.Daemon.Socket),
		scanner:      worktree.NewScanner(cfg, st),
		provisioner:  worktree.NewProvisioner(cfg, st),
		migrator:     worktree.NewMigrator(cfg, st),
		publisher:    publisher,
		spawner:      agent.NewSpawner(cfg, st, publisher),
		taskRegistry: taskRegistry,
		agents:       make(map[string]*AgentProcess),
		jobQueue:     make(chan string, 100),
		ctx:          ctx,
		cancel:       cancel,
	}
	d.executor = NewJobExecutor(d)

	// Wire up stream event emission from spawner to control server
	d.spawner.SetStreamEmitter(func(eventType, agentID, worktreePath string, payload any) {
		d.emitAgentStreamEvent(eventType, agentID, worktreePath, payload)
	})

	d.registerHandlers()
	return d, nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	// Start control server
	if err := d.server.Start(); err != nil {
		return err
	}
	logging.Info("control server listening", "socket", d.config.Daemon.Socket)

	// Data is cached in SQLite - no blocking scan needed
	// Start background scan to pick up any new repos
	d.safeGo("initial-scan", func() {
		if err := d.scanner.ScanAndStore(); err != nil {
			logging.Warn("background scan failed", "error", err)
		}
	})

	// Reconcile running agents from previous session
	d.reconcileAgents()

	// Pick up any pending jobs from previous session
	d.requeuePendingJobs()

	// Start background workers
	d.wg.Add(3)
	go d.safeLoop("scan-loop", d.scanLoop)
	go d.safeLoop("health-check-loop", d.healthCheckLoop)
	go d.safeLoop("job-execution-loop", d.jobExecutionLoop)

	// Set up signal handling
	sigCh := make(chan os.Signal, 2) // Buffer of 2 for second signal
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	return d.signalLoop(sigCh)
}

// signalLoop handles OS signals for graceful shutdown.
func (d *Daemon) signalLoop(sigCh <-chan os.Signal) error {
	for {
		sig := <-sigCh

		switch sig {
		case syscall.SIGHUP:
			// Config reload (optional feature)
			logging.Info("received SIGHUP, reloading config")
			if err := d.reloadConfig(); err != nil {
				logging.Error("config reload failed", "error", err)
			}

		case syscall.SIGINT, syscall.SIGTERM:
			logging.Info("received shutdown signal, starting graceful shutdown", "signal", sig.String())

			// Start graceful shutdown
			shutdownDone := make(chan struct{})
			go func() {
				d.gracefulShutdown()
				close(shutdownDone)
			}()

			// Wait for graceful shutdown or second signal
			select {
			case <-shutdownDone:
				logging.Info("graceful shutdown complete")
				return nil

			case sig2 := <-sigCh:
				// Second signal - force immediate exit
				logging.Warn("received second signal, forcing immediate shutdown", "signal", sig2.String())
				d.forceShutdown()
				return fmt.Errorf("forced shutdown by signal: %s", sig2.String())
			}
		}
	}
}

// gracefulShutdown performs a clean shutdown with work draining.
func (d *Daemon) gracefulShutdown() {
	d.shutdownOnce.Do(func() {
		// Mark as draining - stop accepting new work
		d.setDraining(true)
		logging.Info("stopped accepting new work, draining in-flight jobs")

		// Stop the control server from accepting new connections
		// but allow existing connections to finish
		d.server.Stop()

		// Cancel context to signal workers to stop
		d.cancel()

		// Wait for workers with timeout
		done := make(chan struct{})
		go func() {
			d.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logging.Info("all workers stopped")
		case <-time.After(DrainTimeout):
			logging.Warn("drain timeout exceeded, some jobs may not have completed")
		}

		// Terminate any remaining agent processes
		d.terminateAllAgents()

		// Close database
		if err := d.store.Close(); err != nil {
			logging.Error("error closing database", "error", err)
		}

		// Flush Sentry events
		logging.Info("flushing Sentry events")
		logging.Flush(2 * time.Second)
	})
}

// forceShutdown performs an immediate shutdown without waiting.
func (d *Daemon) forceShutdown() {
	// Terminate all agent processes immediately
	d.terminateAllAgents()

	// Force close server and database
	d.server.Stop()
	d.store.Close()

	// Flush Sentry with short timeout
	logging.Flush(500 * time.Millisecond)
}

// terminateAllAgents kills all running agent processes.
func (d *Daemon) terminateAllAgents() {
	d.agentsMu.Lock()
	defer d.agentsMu.Unlock()

	for id, proc := range d.agents {
		logging.Info("terminating agent", "agent_id", id, "pid", proc.PID)
		proc.Cancel()

		// Also try to kill the process directly
		if p, err := os.FindProcess(proc.PID); err == nil {
			p.Signal(syscall.SIGTERM)

			// Give it a moment, then force kill
			time.AfterFunc(5*time.Second, func() {
				p.Signal(syscall.SIGKILL)
			})
		}
	}
	d.agents = make(map[string]*AgentProcess)
}

// reloadConfig handles SIGHUP for config reload.
func (d *Daemon) reloadConfig() error {
	newCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Update configuration (only safe fields)
	d.config.Repos.ScanInterval = newCfg.Repos.ScanInterval
	d.config.Agents.HeartbeatInterval = newCfg.Agents.HeartbeatInterval

	logging.Info("config reloaded",
		"scan_interval", d.config.Repos.ScanInterval,
		"heartbeat_interval", d.config.Agents.HeartbeatInterval)

	return nil
}

// setDraining sets the draining state.
func (d *Daemon) setDraining(draining bool) {
	d.drainingMu.Lock()
	d.draining = draining
	d.drainingMu.Unlock()
}

// isDraining returns true if the daemon is draining.
func (d *Daemon) isDraining() bool {
	d.drainingMu.RLock()
	defer d.drainingMu.RUnlock()
	return d.draining
}

// safeGo runs a function in a goroutine with panic recovery.
func (d *Daemon) safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logging.CapturePanic(r, "goroutine", name)
			}
		}()
		fn()
	}()
}

// safeLoop wraps a loop function with panic recovery.
// If the loop panics, it logs to Sentry and exits gracefully.
func (d *Daemon) safeLoop(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logging.CapturePanic(r, "loop", name)
			// Signal other goroutines to stop
			d.cancel()
		}
	}()
	fn()
}

// requeuePendingJobs picks up jobs that were pending when daemon stopped.
func (d *Daemon) requeuePendingJobs() {
	jobs, err := d.store.ListPendingJobs()
	if err != nil {
		logging.Warn("failed to list pending jobs", "error", err)
		return
	}
	for _, job := range jobs {
		select {
		case d.jobQueue <- job.ID:
			logging.Debug("requeued pending job", "job_id", job.ID)
		default:
			logging.Warn("job queue full, couldn't requeue", "job_id", job.ID)
		}
	}
}

// jobExecutionLoop processes jobs from the queue.
func (d *Daemon) jobExecutionLoop() {
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return
		case jobID := <-d.jobQueue:
			job, err := d.store.GetJob(jobID)
			if err != nil || job == nil {
				logging.Warn("couldn't load job", "job_id", jobID, "error", err)
				continue
			}
			if job.Status != store.JobStatusPending {
				continue // Already processed
			}
			logging.Info("executing job",
				"job_id", job.ID,
				"type", job.Type,
				"input", truncateForLog(job.NormalizedInput, 50))
			if err := d.executor.ExecuteJob(d.ctx, job); err != nil {
				logging.Error("job failed", "job_id", job.ID, "error", err)
			}
		}
	}
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func (d *Daemon) registerHandlers() {
	d.server.Handle("list_agents", d.handleListAgents)
	d.server.Handle("get_agent", d.handleGetAgent)
	d.server.Handle("get_agent_logs", d.handleGetAgentLogs)
	d.server.Handle("spawn_agent", d.handleSpawnAgent)
	d.server.Handle("kill_agent", d.handleKillAgent)
	d.server.Handle("list_worktrees", d.handleListWorktrees)
	d.server.Handle("create_worktree", d.handleCreateWorktree)
	d.server.Handle("list_jobs", d.handleListJobs)
	d.server.Handle("create_job", d.handleCreateJob)
	d.server.Handle("rescan", d.handleRescan)
	d.server.Handle("normalize_plan", d.handleNormalizePlan)
	d.server.Handle("normalize", d.handleNormalize)
	// Migration
	d.server.Handle("migrate_plan", d.handleMigratePlan)
	d.server.Handle("migrate_worktrees", d.handleMigrateWorktrees)
	// Notes
	d.server.Handle("list_notes", d.handleListNotes)
	d.server.Handle("create_note", d.handleCreateNote)
	d.server.Handle("update_note", d.handleUpdateNote)
	d.server.Handle("delete_note", d.handleDeleteNote)
	// Changelog
	d.server.Handle("list_changelog", d.handleListChangelog)
	d.server.Handle("create_changelog", d.handleCreateChangelog)
	d.server.Handle("delete_changelog", d.handleDeleteChangelog)
	// Plans
	d.server.Handle("get_plan", d.handleGetPlan)
	d.server.Handle("approve_plan", d.handleApprovePlan)
	d.server.Handle("spawn_executor", d.handleSpawnExecutor)
	// Publish/Merge/Cleanup/Abandon
	d.server.Handle("publish_pr", d.handlePublishPR)
	d.server.Handle("merge_local", d.handleMergeLocal)
	d.server.Handle("cleanup_worktree", d.handleCleanupWorktree)
	d.server.Handle("abandon_worktree", d.handleAbandonWorktree)
	// Context (blackboard + state)
	d.server.Handle("get_blackboard", d.handleGetBlackboard)
	d.server.Handle("post_blackboard", d.handlePostBlackboard)
	d.server.Handle("clear_blackboard", d.handleClearBlackboard)
	d.server.Handle("get_blackboard_summary", d.handleGetBlackboardSummary)
	d.server.Handle("get_project_state", d.handleGetProjectState)
	d.server.Handle("set_project_state", d.handleSetProjectState)
	d.server.Handle("get_state_summary", d.handleGetStateSummary)
	d.server.Handle("get_context_preview", d.handleGetContextPreview)
	// Metrics
	d.server.Handle("get_metrics_trend", d.handleGetMetricsTrend)
	d.server.Handle("get_cache_stats", d.handleGetCacheStats)
	// Streaming (for athena-viz)
	d.server.HandleStream("subscribe_stream", d.handleSubscribeStream)
	// Tasks (Claude Code integration)
	d.server.Handle("list_task_providers", d.handleListTaskProviders)
	d.server.Handle("list_task_lists", d.handleListTaskLists)
	d.server.Handle("list_tasks", d.handleListTasks)
	d.server.Handle("get_task", d.handleGetTask)
	d.server.Handle("create_task", d.handleCreateTask)
	d.server.Handle("update_task", d.handleUpdateTask)
	d.server.Handle("delete_task", d.handleDeleteTask)
	d.server.Handle("execute_task", d.handleExecuteTask)
	d.server.Handle("broadcast_task", d.handleBroadcastTask)
}

func (d *Daemon) handleListWorktrees(_ json.RawMessage) (any, error) {
	worktrees, err := d.store.ListWorktrees("")
	if err != nil {
		return nil, err
	}

	var result []*control.WorktreeInfo
	for _, wt := range worktrees {
		info := &control.WorktreeInfo{
			Path:     wt.Path,
			Project:  wt.Project,
			Branch:   wt.Branch,
			IsMain:   wt.IsMain,
			WTStatus: string(wt.Status),
		}
		if wt.AgentID != nil {
			info.AgentID = *wt.AgentID
		}
		if wt.TicketID != nil {
			info.TicketID = *wt.TicketID
		}
		if wt.TicketHash != nil {
			info.TicketHash = *wt.TicketHash
		}
		if wt.Description != nil {
			info.Description = *wt.Description
		}
		if wt.ProjectName != nil {
			info.ProjectName = *wt.ProjectName
		}
		if wt.PRURL != nil {
			info.PRURL = *wt.PRURL
		}
		if wt.SourceNoteID != nil {
			info.SourceNoteID = *wt.SourceNoteID
		}

		// Get plan summary for this worktree
		if summary, err := d.store.GetPlanSummary(wt.Path); err == nil && summary != "" {
			info.Summary = summary
		}

		// Get git status
		status, _ := d.provisioner.GetStatus(wt.Path)
		if status != nil {
			if status.Clean {
				info.Status = "clean"
			} else {
				info.Status = fmt.Sprintf("+%d ~%d", status.Modified, status.Staged)
			}
		}

		result = append(result, info)
	}
	return result, nil
}

func (d *Daemon) handleRescan(_ json.RawMessage) (any, error) {
	if err := d.scanner.ScanAndStore(); err != nil {
		return nil, err
	}
	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleNormalizePlan(_ json.RawMessage) (any, error) {
	plan, err := d.provisioner.PlanNormalize()
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (d *Daemon) handleNormalize(_ json.RawMessage) (any, error) {
	moved, err := d.provisioner.Normalize(false)
	if err != nil {
		return nil, err
	}

	// Rescan to update store
	d.scanner.ScanAndStore()

	// Broadcast update
	d.server.Broadcast(control.Event{
		Type:    "worktrees_normalized",
		Payload: map[string]any{"moved": moved},
	})

	return map[string]any{"moved": moved}, nil
}

func (d *Daemon) reconcileAgents() {
	agents, err := d.store.ListRunningAgents()
	if err != nil {
		logging.Error("failed to list running agents", "error", err)
		return
	}

	for _, agent := range agents {
		if agent.PID != nil {
			// Check if process is still running
			if processExists(*agent.PID) {
				logging.Info("reattaching to agent", "agent_id", agent.ID, "pid", *agent.PID)
				if err := d.spawner.Attach(agent.ID); err != nil {
					logging.Error("failed to reattach to agent", "agent_id", agent.ID, "error", err)
					// If reattach fails, we can't do much, maybe mark as crashed if we think it's gone
				}
			} else {
				logging.Warn("agent not running, marking crashed",
					"agent_id", agent.ID,
					"pid", *agent.PID)
				d.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
			}
		} else {
			// Agent was pending or spawning when daemon stopped
			logging.Debug("resetting agent to pending", "agent_id", agent.ID)
			d.store.UpdateAgentStatus(agent.ID, store.AgentStatusPending)
		}
	}
}

func (d *Daemon) scanLoop() {
	defer d.wg.Done()

	if d.config.Repos.ScanInterval == 0 {
		return // Disabled
	}

	ticker := time.NewTicker(d.config.Repos.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if err := d.scanner.ScanAndStore(); err != nil {
				logging.Warn("scan error", "error", err)
			}
		}
	}
}

func (d *Daemon) healthCheckLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.Agents.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.checkAgentHealth()
		}
	}
}

// awaitingTimeout is how long without a heartbeat before marking agent as awaiting.
const awaitingTimeout = 30 * time.Second

func (d *Daemon) checkAgentHealth() {
	agents, err := d.store.ListRunningAgents()
	if err != nil {
		return
	}

	d.agentsMu.RLock()
	defer d.agentsMu.RUnlock()

	now := time.Now()

	for _, agent := range agents {
		if agent.PID == nil {
			continue
		}

		// Check if process is still running
		if !processExists(*agent.PID) {
			d.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
			d.server.Broadcast(control.Event{
				Type:    "agent_crashed",
				Payload: map[string]string{"id": agent.ID},
			})
			continue
		}

		// Check for awaiting status (process running but no recent heartbeat)
		// Only apply to active states (planning, executing, running)
		if agent.Status == store.AgentStatusPlanning ||
			agent.Status == store.AgentStatusExecuting ||
			agent.Status == store.AgentStatusRunning {

			if agent.LastHeartbeat != nil {
				timeSinceHeartbeat := now.Sub(*agent.LastHeartbeat)
				if timeSinceHeartbeat > awaitingTimeout {
					// Agent is idle - likely waiting for user input
					d.store.UpdateAgentStatus(agent.ID, store.AgentStatusAwaiting)
					d.server.Broadcast(control.Event{
						Type:    "agent_awaiting",
						Payload: map[string]string{"id": agent.ID},
					})
					logging.Debug("agent marked as awaiting",
						"agent_id", agent.ID,
						"last_heartbeat", agent.LastHeartbeat,
						"idle_seconds", timeSinceHeartbeat.Seconds())
				}
			}
		}
	}
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, signal 0 checks if process exists
	return process.Signal(syscall.Signal(0)) == nil
}

// Note handlers

func (d *Daemon) handleListNotes(_ json.RawMessage) (any, error) {
	notes, err := d.store.ListNotes()
	if err != nil {
		return nil, err
	}

	var result []*control.NoteInfo
	for _, n := range notes {
		result = append(result, noteToInfo(n))
	}
	return result, nil
}

func (d *Daemon) handleCreateNote(params json.RawMessage) (any, error) {
	var req control.CreateNoteRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	note := &store.Note{
		ID:      generateID(),
		Content: req.Content,
		Done:    false,
	}

	if err := d.store.CreateNote(note); err != nil {
		return nil, err
	}

	return noteToInfo(note), nil
}

func (d *Daemon) handleUpdateNote(params json.RawMessage) (any, error) {
	var req control.UpdateNoteRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.UpdateNoteDone(req.ID, req.Done); err != nil {
		return nil, err
	}

	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleDeleteNote(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.DeleteNote(req.ID); err != nil {
		return nil, err
	}

	return map[string]bool{"success": true}, nil
}

func noteToInfo(n *store.Note) *control.NoteInfo {
	return &control.NoteInfo{
		ID:        n.ID,
		Content:   n.Content,
		Done:      n.Done,
		CreatedAt: n.CreatedAt.Format(time.RFC3339),
	}
}

// Changelog handlers

func (d *Daemon) handleListChangelog(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		// No params is fine, use defaults
		req.Limit = 100
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}

	entries, err := d.store.ListChangelog(req.Project, req.Limit)
	if err != nil {
		return nil, err
	}

	var result []*control.ChangelogInfo
	for _, e := range entries {
		result = append(result, changelogToInfo(e))
	}
	return result, nil
}

func (d *Daemon) handleCreateChangelog(params json.RawMessage) (any, error) {
	var req control.CreateChangelogRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	entry := &store.ChangelogEntry{
		ID:          generateID(),
		Title:       req.Title,
		Description: req.Description,
		Category:    req.Category,
		Project:     req.Project,
	}
	if req.JobID != "" {
		entry.JobID = &req.JobID
	}
	if req.AgentID != "" {
		entry.AgentID = &req.AgentID
	}

	// Default category
	if entry.Category == "" {
		entry.Category = "feature"
	}

	if err := d.store.CreateChangelogEntry(entry); err != nil {
		return nil, err
	}

	return changelogToInfo(entry), nil
}

func (d *Daemon) handleDeleteChangelog(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.DeleteChangelogEntry(req.ID); err != nil {
		return nil, err
	}

	return map[string]bool{"success": true}, nil
}

func changelogToInfo(e *store.ChangelogEntry) *control.ChangelogInfo {
	info := &control.ChangelogInfo{
		ID:          e.ID,
		Title:       e.Title,
		Description: e.Description,
		Category:    e.Category,
		Project:     e.Project,
		CreatedAt:   e.CreatedAt.Format(time.RFC3339),
	}
	if e.JobID != nil {
		info.JobID = *e.JobID
	}
	if e.AgentID != nil {
		info.AgentID = *e.AgentID
	}
	return info
}

// Migration handlers

func (d *Daemon) handleMigratePlan(_ json.RawMessage) (any, error) {
	plan, err := d.migrator.PlanMigration()
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (d *Daemon) handleMigrateWorktrees(params json.RawMessage) (any, error) {
	var req struct {
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		// No params is fine, use defaults
		req.DryRun = false
	}

	migrated, err := d.migrator.Migrate(req.DryRun)
	if err != nil {
		return nil, err
	}

	// Rescan to update store
	d.scanner.ScanAndStore()

	// Broadcast update
	d.server.Broadcast(control.Event{
		Type:    "worktrees_migrated",
		Payload: map[string]any{"migrated": migrated, "dry_run": req.DryRun},
	})

	return map[string]any{"migrated": migrated, "dry_run": req.DryRun}, nil
}

// Worktree creation handler

func (d *Daemon) handleCreateWorktree(params json.RawMessage) (any, error) {
	var req control.CreateWorktreeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Validate main repo exists
	mainRepo, err := d.store.GetWorktree(req.MainRepoPath)
	if err != nil {
		return nil, err
	}
	if mainRepo == nil || !mainRepo.IsMain {
		return nil, fmt.Errorf("main repo not found: %s", req.MainRepoPath)
	}

	// Create worktree
	opts := worktree.CreateWorktreeOptions{
		MainRepoPath: req.MainRepoPath,
		Branch:       req.Branch,
		TicketID:     req.TicketID,
		Description:  req.Description,
		WorkflowMode: req.WorkflowMode,
		SourceNoteID: req.SourceNoteID,
	}

	path, err := d.migrator.CreateWorktree(opts)
	if err != nil {
		return nil, err
	}

	// Fetch the created worktree from store
	wt, err := d.store.GetWorktree(path)
	if err != nil || wt == nil {
		// Return basic info if store fetch fails
		return &control.WorktreeInfo{
			Path:     path,
			Project:  mainRepo.Project,
			Branch:   req.Branch,
			TicketID: req.TicketID,
		}, nil
	}

	// Build response
	info := &control.WorktreeInfo{
		Path:     wt.Path,
		Project:  wt.Project,
		Branch:   wt.Branch,
		IsMain:   wt.IsMain,
		WTStatus: string(wt.Status),
	}
	if wt.TicketID != nil {
		info.TicketID = *wt.TicketID
	}
	if wt.TicketHash != nil {
		info.TicketHash = *wt.TicketHash
	}
	if wt.Description != nil {
		info.Description = *wt.Description
	}

	// Broadcast worktree created event
	d.server.Broadcast(control.Event{
		Type:    "worktree_created",
		Payload: info,
	})

	// In manual mode, don't auto-spawn a planning agent
	// In automatic and approve modes, spawn the planner
	if req.WorkflowMode == string(config.WorkflowModeManual) {
		logging.Info("manual mode: skipping auto-spawn of planning agent", "worktree", path)
		return info, nil
	}

	// Auto-spawn a planning agent for the new worktree
	description := req.Description
	if description == "" {
		description = "New feature worktree"
	}
	planPrompt := fmt.Sprintf(`You are a planning agent. Analyze the following feature request and create a detailed implementation plan.

Feature Request: %s

Instructions:
1. Explore the codebase to understand the architecture and patterns
2. Identify the files that need to be modified or created
3. Use the EnterPlanMode tool to create your plan

IMPORTANT: Start your plan with YAML frontmatter containing a brief summary:
---
summary: One sentence describing what will be implemented
---

Then write the full plan with:
- Overview of the feature
- Step-by-step implementation plan
- Files to modify/create
- Testing considerations
- Potential risks or edge cases

Do NOT make any code changes. Only explore and create the plan.`, description)

	spec := agent.SpawnSpec{
		WorktreePath: path,
		ProjectName:  wt.Project,
		Archetype:    "planner",
		Prompt:       planPrompt,
		Provider:     req.Provider,
	}

	spawnedAgent, err := d.spawner.Spawn(d.ctx, spec)
	if err != nil {
		logging.Warn("failed to auto-spawn planning agent", "worktree", path, "error", err)
		// Don't fail the worktree creation, just log the error
	} else {
		// Associate agent with worktree
		d.store.AssignAgentToWorktree(path, spawnedAgent.ID)
		info.AgentID = spawnedAgent.ID

		// Broadcast agent created event
		d.server.Broadcast(control.Event{
			Type:    "agent_created",
			Payload: d.agentToInfo(spawnedAgent),
		})

		logging.Info("auto-spawned planning agent",
			"worktree", path,
			"agent_id", spawnedAgent.ID,
			"session_id", spawnedAgent.ClaudeSessionID)
	}

	return info, nil
}

// Plan handlers

func (d *Daemon) handleGetPlan(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
		ForceRefresh bool   `json:"force_refresh"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Find planner agent for this worktree
	agents, _ := d.store.ListAgentsByWorktree(req.WorktreePath)
	var plannerAgent *store.Agent
	for _, a := range agents {
		if a.Archetype == "planner" {
			plannerAgent = a
			break
		}
	}

	// Check DB cache first (unless force refresh)
	if !req.ForceRefresh {
		plan, err := d.store.GetPlan(req.WorktreePath)
		if err == nil && plan != nil && plan.Status != store.PlanStatusPending {
			// Return cached plan if we have content
			info := planToInfo(plan)
			if plannerAgent != nil {
				info.PlannerStatus = string(plannerAgent.Status)
			}
			return info, nil
		}
	}

	// Try to read from Claude's native plan storage
	// Claude stores plans at ~/.claude/plans/<slug>.md
	if plannerAgent != nil && plannerAgent.ClaudeSessionID != "" {
		content, err := readClaudePlan(req.WorktreePath, plannerAgent.ClaudeSessionID)
		if err == nil && content != "" {
			// Parse frontmatter to extract summary
			summary, _ := parsePlanFrontmatter(content)

			// Plan found - cache in DB and return
			existingPlan, _ := d.store.GetPlan(req.WorktreePath)
			if existingPlan != nil {
				d.store.UpdatePlanContent(req.WorktreePath, content)
				if summary != "" && existingPlan.Summary != summary {
					d.store.UpdatePlanSummary(req.WorktreePath, summary)
				}
				if existingPlan.Status == store.PlanStatusPending {
					d.store.UpdatePlanStatus(req.WorktreePath, store.PlanStatusDraft)
				}
				existingPlan.Content = content
				existingPlan.Summary = summary
				existingPlan.Status = store.PlanStatusDraft
				info := planToInfo(existingPlan)
				info.PlannerStatus = string(plannerAgent.Status)
				return info, nil
			}

			// Create new plan
			plan := &store.Plan{
				ID:           generateID(),
				WorktreePath: req.WorktreePath,
				AgentID:      plannerAgent.ID,
				Content:      content,
				Summary:      summary,
				Status:       store.PlanStatusDraft,
			}
			if err := d.store.CreatePlan(plan); err != nil {
				return nil, fmt.Errorf("failed to cache plan: %w", err)
			}
			info := planToInfo(plan)
			info.PlannerStatus = string(plannerAgent.Status)
			return info, nil
		}
		// Log why plan wasn't found (helpful for debugging)
		logging.Debug("could not read Claude plan", "error", err, "session_id", plannerAgent.ClaudeSessionID)
	}

	// No plan yet - check if planner is still working
	if plannerAgent == nil {
		return nil, fmt.Errorf("no planner agent found for worktree")
	}

	// Planner is working but hasn't created plan yet
	// Return a "pending" plan so the TUI can show progress
	plan := &store.Plan{
		ID:           generateID(),
		WorktreePath: req.WorktreePath,
		AgentID:      plannerAgent.ID,
		Content:      "", // No content yet
		Status:       store.PlanStatusPending,
	}

	// Cache the pending plan
	existingPlan, _ := d.store.GetPlan(req.WorktreePath)
	if existingPlan == nil {
		d.store.CreatePlan(plan)
	}

	info := planToInfo(plan)
	info.PlannerStatus = string(plannerAgent.Status)
	return info, nil
}

func (d *Daemon) handleApprovePlan(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.UpdatePlanStatus(req.WorktreePath, store.PlanStatusApproved); err != nil {
		return nil, err
	}

	return map[string]bool{"success": true}, nil
}

// TickLoop runs periodic tasks.

func planToInfo(p *store.Plan) *control.PlanInfo {
	return &control.PlanInfo{
		ID:           p.ID,
		WorktreePath: p.WorktreePath,
		AgentID:      p.AgentID,
		Content:      p.Content,
		Summary:      p.Summary,
		Status:       string(p.Status),
		CreatedAt:    p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    p.UpdatedAt.Format(time.RFC3339),
	}
}

// readClaudePlan reads the plan content from Claude's native plan storage.
// Claude stores plans at ~/.claude/plans/<slug>.md where the slug is found
// in the session's jsonl file.
func readClaudePlan(worktreePath, sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("no session ID")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot get home dir: %w", err)
	}

	// Claude stores projects at ~/.claude/projects/<escaped-path>/
	// where path separators are replaced with dashes
	escapedPath := strings.ReplaceAll(worktreePath, "/", "-")
	sessionFile := filepath.Join(homeDir, ".claude", "projects", escapedPath, sessionID+".jsonl")

	// Read session file to extract slug
	slug, err := extractSessionSlug(sessionFile)
	if err != nil {
		return "", fmt.Errorf("cannot extract slug: %w", err)
	}

	// Read plan from Claude's plans directory
	planPath := filepath.Join(homeDir, ".claude", "plans", slug+".md")
	content, err := os.ReadFile(planPath)
	if err != nil {
		return "", fmt.Errorf("plan not found at %s: %w", planPath, err)
	}

	return string(content), nil
}

// extractSessionSlug extracts the "slug" field from a Claude session jsonl file.
// The slug determines the plan filename in ~/.claude/plans/
func extractSessionSlug(sessionFile string) (string, error) {
	f, err := os.Open(sessionFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large JSON lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// Quick check before parsing
		if !strings.Contains(line, `"slug"`) {
			continue
		}

		var entry struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Slug != "" {
			return entry.Slug, nil
		}
	}

	return "", fmt.Errorf("no slug found in session file")
}

// Publish/Merge/Cleanup handlers

func (d *Daemon) handlePublishPR(params json.RawMessage) (any, error) {
	var req control.PublishPRRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	opts := worktree.PublishOptions{
		WorktreePath: req.WorktreePath,
		Title:        req.Title,
		Body:         req.Body,
	}

	result, err := d.publisher.PublishPR(opts)
	if err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type: "worktree_published",
		Payload: map[string]string{
			"path":   req.WorktreePath,
			"pr_url": result.PRURL,
			"branch": result.Branch,
		},
	})

	return &control.PublishResult{
		PRURL:  result.PRURL,
		Branch: result.Branch,
	}, nil
}

func (d *Daemon) handleMergeLocal(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	result, err := d.publisher.MergeLocal(req.WorktreePath)
	if err != nil {
		return nil, err
	}

	// If merge had conflicts, spawn a resolver agent
	if result.HasConflicts {
		logging.Info("spawning conflict resolver agent", "worktree", req.WorktreePath, "branch", result.Branch)

		// Get worktree for project info
		wt, _ := d.store.GetWorktree(req.WorktreePath)
		projectName := ""
		if wt != nil {
			projectName = wt.Project
		}

		// Create a merge resolution job/task
		taskDescription := fmt.Sprintf("Resolve merge conflicts: %s → main", result.Branch)
		job := &store.Job{
			ID:              generateID(),
			RawInput:        taskDescription,
			NormalizedInput: taskDescription,
			Status:          store.JobStatusExecuting,
			Type:            store.JobTypeMerge,
			Project:         projectName,
		}
		if err := d.store.CreateJob(job); err != nil {
			logging.Warn("failed to create merge job", "error", err)
		} else {
			// Broadcast job creation
			d.server.Broadcast(control.Event{
				Type:    "job_created",
				Payload: jobToInfo(job),
			})
		}

		// Spawn a Sonnet agent to resolve the conflicts via rebase
		spec := agent.SpawnSpec{
			WorktreePath: req.WorktreePath,
			ProjectName:  projectName,
			Archetype:    "resolver", // Uses sonnet model
			Prompt: fmt.Sprintf(`Your worktree branch '%s' has conflicts with main.

Your task:
1. Rebase your branch onto the latest main: git fetch origin && git rebase origin/main
2. For each conflict, resolve it intelligently by understanding both changes
3. After resolving all conflicts, continue the rebase: git rebase --continue
4. Once complete, commit any final changes
5. Report what conflicts were resolved and how

If the conflicts are too complex to resolve automatically, explain what manual intervention is needed.`, result.Branch),
		}

		spawnedAgent, spawnErr := d.spawner.Spawn(d.ctx, spec)
		if spawnErr != nil {
			logging.Error("failed to spawn conflict resolver", "error", spawnErr)
			// Mark job as failed
			if job != nil {
				d.store.UpdateJobStatus(job.ID, store.JobStatusFailed)
			}
			return &control.MergeLocalResult{
				Success:      false,
				HasConflicts: true,
				Message:      result.Message + " (failed to spawn resolver agent)",
			}, nil
		}

		// Associate agent with worktree
		d.store.AssignAgentToWorktree(req.WorktreePath, spawnedAgent.ID)

		// Broadcast agent creation
		d.server.Broadcast(control.Event{
			Type:    "agent_created",
			Payload: d.agentToInfo(spawnedAgent),
		})

		return &control.MergeLocalResult{
			Success:      false,
			HasConflicts: true,
			AgentSpawned: true,
			AgentID:      spawnedAgent.ID,
			Message:      result.Message + " - resolver agent spawned",
		}, nil
	}

	// Broadcast success event
	d.server.Broadcast(control.Event{
		Type: "worktree_merged",
		Payload: map[string]string{
			"path": req.WorktreePath,
		},
	})

	return &control.MergeLocalResult{
		Success: true,
		Message: result.Message,
	}, nil
}

func (d *Daemon) handleCleanupWorktree(params json.RawMessage) (any, error) {
	var req control.CleanupWorktreeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.publisher.Cleanup(req.WorktreePath, req.DeleteBranch); err != nil {
		return nil, err
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type: "worktree_cleaned",
		Payload: map[string]string{
			"path": req.WorktreePath,
		},
	})

	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleAbandonWorktree(params json.RawMessage) (any, error) {
	var req control.AbandonWorktreeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Kill any running agent process before abandoning
	wt, _ := d.store.GetWorktree(req.WorktreePath)
	if wt != nil && wt.AgentID != nil {
		if err := d.spawner.Kill(*wt.AgentID); err != nil {
			logging.Debug("spawner kill returned error (may not be running)", "agent_id", *wt.AgentID, "error", err)
		}
	}

	if err := d.publisher.Abandon(req.WorktreePath); err != nil {
		return nil, err
	}

	// Broadcast event for TUI updates
	d.server.Broadcast(control.Event{
		Type: "worktree_abandoned",
		Payload: map[string]string{
			"path": req.WorktreePath,
		},
	})

	return map[string]bool{"success": true}, nil
}

// parsePlanFrontmatter extracts the summary from YAML frontmatter in a plan.
// Frontmatter is delimited by --- at the start and end:
//
//	---
//	summary: Brief description of the plan
//	---
//	# Plan content...
func parsePlanFrontmatter(content string) (summary string, body string) {
	// Check for frontmatter delimiter
	if !strings.HasPrefix(content, "---") {
		return "", content
	}

	// Find the closing delimiter
	lines := strings.SplitN(content, "\n", -1)
	if len(lines) < 3 {
		return "", content
	}

	// Find the end of frontmatter (second ---)
	endIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIndex = i
			break
		}
	}

	if endIndex == -1 {
		return "", content
	}

	// Extract and parse the YAML frontmatter
	frontmatterYAML := strings.Join(lines[1:endIndex], "\n")
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &frontmatter); err != nil {
		return "", content
	}

	// Extract summary field
	if s, ok := frontmatter["summary"].(string); ok {
		summary = s
	}

	// Body is everything after the closing delimiter
	body = strings.Join(lines[endIndex+1:], "\n")
	body = strings.TrimPrefix(body, "\n") // Remove leading newline

	return summary, body
}

// Context handlers (blackboard + state)

func (d *Daemon) handleGetBlackboard(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	entries, err := d.store.ListBlackboardEntries(req.WorktreePath)
	if err != nil {
		return nil, err
	}

	var result []*control.BlackboardEntryInfo
	for _, e := range entries {
		result = append(result, blackboardEntryToInfo(e))
	}
	return result, nil
}

func (d *Daemon) handlePostBlackboard(params json.RawMessage) (any, error) {
	var req control.PostBlackboardRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Default agent ID for manual entries
	agentID := req.AgentID
	if agentID == "" {
		agentID = "manual"
	}

	// Map entry type string to store type
	entryType := store.BlackboardEntryType(req.EntryType)
	if !isValidBlackboardEntryType(entryType) {
		return nil, fmt.Errorf("invalid entry type: %s", req.EntryType)
	}

	entry := &store.BlackboardEntry{
		ID:           actx.GenerateEntryID(),
		WorktreePath: req.WorktreePath,
		EntryType:    entryType,
		Content:      req.Content,
		AgentID:      agentID,
	}

	if err := d.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}

	return blackboardEntryToInfo(entry), nil
}

func (d *Daemon) handleClearBlackboard(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if err := d.store.ClearBlackboard(req.WorktreePath); err != nil {
		return nil, err
	}

	return map[string]bool{"success": true}, nil
}

func (d *Daemon) handleGetBlackboardSummary(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	counts, err := d.store.CountBlackboardEntries(req.WorktreePath)
	if err != nil {
		return nil, err
	}

	// Calculate totals
	total := 0
	for _, c := range counts {
		total += c
	}

	// Get unresolved question count
	unresolvedCount, _ := d.store.CountUnresolvedQuestions(req.WorktreePath)

	return &control.BlackboardSummaryInfo{
		WorktreePath:    req.WorktreePath,
		DecisionCount:   counts[store.BlackboardTypeDecision],
		FindingCount:    counts[store.BlackboardTypeFinding],
		AttemptCount:    counts[store.BlackboardTypeAttempt],
		QuestionCount:   counts[store.BlackboardTypeQuestion],
		ArtifactCount:   counts[store.BlackboardTypeArtifact],
		UnresolvedCount: unresolvedCount,
		TotalCount:      total,
	}, nil
}

func (d *Daemon) handleGetProjectState(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	entries, err := d.store.ListStateEntries(req.Project)
	if err != nil {
		return nil, err
	}

	var result []*control.StateEntryInfo
	for _, e := range entries {
		result = append(result, stateEntryToInfo(e))
	}
	return result, nil
}

func (d *Daemon) handleSetProjectState(params json.RawMessage) (any, error) {
	var req control.SetStateRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Map state type string to store type
	stateType := store.StateEntryType(req.StateType)
	if !isValidStateEntryType(stateType) {
		return nil, fmt.Errorf("invalid state type: %s", req.StateType)
	}

	// Default confidence
	confidence := req.Confidence
	if confidence <= 0 {
		confidence = 1.0
	}

	// Optional source agent
	var agentID *string
	if req.AgentID != "" {
		agentID = &req.AgentID
	}

	entry := &store.StateEntry{
		ID:          actx.GenerateEntryID(),
		Project:     req.Project,
		StateType:   stateType,
		Key:         req.Key,
		Value:       req.Value,
		Confidence:  confidence,
		SourceAgent: agentID,
	}

	if err := d.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}

	// Re-fetch to get timestamps
	entry, err := d.store.GetStateEntryByKey(req.Project, stateType, req.Key)
	if err != nil {
		return nil, err
	}

	return stateEntryToInfo(entry), nil
}

func (d *Daemon) handleGetStateSummary(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	entries, err := d.store.ListStateEntries(req.Project)
	if err != nil {
		return nil, err
	}

	// Calculate summary stats
	summary := &control.StateSummaryInfo{
		Project: req.Project,
	}
	var totalConfidence float64
	for _, e := range entries {
		summary.TotalCount++
		totalConfidence += e.Confidence

		switch e.StateType {
		case store.StateTypeArchitecture:
			summary.ArchitectureCount++
		case store.StateTypeConvention:
			summary.ConventionCount++
		case store.StateTypeConstraint:
			summary.ConstraintCount++
		case store.StateTypeDecision:
			summary.DecisionCount++
		case store.StateTypeEnvironment:
			summary.EnvironmentCount++
		}
	}
	if summary.TotalCount > 0 {
		summary.AvgConfidence = totalConfidence / float64(summary.TotalCount)
	}

	return summary, nil
}

func (d *Daemon) handleGetContextPreview(params json.RawMessage) (any, error) {
	var req struct {
		WorktreePath string `json:"worktree_path"`
		ProjectName  string `json:"project_name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Get context manager from spawner
	ctxMgr := d.spawner.ContextManager()
	if ctxMgr == nil {
		return nil, fmt.Errorf("context manager not initialized")
	}

	preview, err := ctxMgr.GetContextPreview(req.WorktreePath, req.ProjectName)
	if err != nil {
		return nil, err
	}

	return map[string]string{"context": preview}, nil
}

func (d *Daemon) handleGetMetricsTrend(params json.RawMessage) (any, error) {
	var req struct {
		Period string `json:"period"` // "today", "week", "all"
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Default to "all" if not specified
	period := req.Period
	if period == "" {
		period = "all"
	}

	trend, err := d.store.GetMetricsTrend(period)
	if err != nil {
		return nil, err
	}

	// Return the store type directly (simple struct)
	return trend, nil
}

func (d *Daemon) handleGetCacheStats(params json.RawMessage) (any, error) {
	var req struct {
		ProjectName string `json:"project_name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if req.ProjectName == "" {
		return nil, fmt.Errorf("project_name is required")
	}

	stats, err := d.store.GetProjectCacheStats(req.ProjectName)
	if err != nil {
		return nil, err
	}

	// Convert to control type
	return &control.ProjectCacheStatsInfo{
		ProjectName:                 stats.ProjectName,
		TotalAgents:                 stats.TotalAgents,
		FirstAgentCount:             stats.FirstAgentCount,
		SubsequentAgentCount:        stats.SubsequentAgentCount,
		AvgCacheHitRate:             stats.AvgCacheHitRate,
		AvgFirstAgentCacheRate:      stats.AvgFirstAgentCacheRate,
		AvgSubsequentAgentCacheRate: stats.AvgSubsequentAgentCacheRate,
		TotalStateTokens:            stats.TotalStateTokens,
		TotalBlackboardTokens:       stats.TotalBlackboardTokens,
		TotalCacheReads:             stats.TotalCacheReads,
	}, nil
}

// Helper functions for context handlers

func blackboardEntryToInfo(e *store.BlackboardEntry) *control.BlackboardEntryInfo {
	info := &control.BlackboardEntryInfo{
		ID:           e.ID,
		WorktreePath: e.WorktreePath,
		EntryType:    string(e.EntryType),
		Content:      e.Content,
		AgentID:      e.AgentID,
		Sequence:     e.Sequence,
		CreatedAt:    e.CreatedAt.Format(time.RFC3339),
		Resolved:     e.Resolved,
	}
	if e.ResolvedBy != nil {
		info.ResolvedBy = *e.ResolvedBy
	}
	return info
}

func stateEntryToInfo(e *store.StateEntry) *control.StateEntryInfo {
	info := &control.StateEntryInfo{
		ID:         e.ID,
		Project:    e.Project,
		StateType:  string(e.StateType),
		Key:        e.Key,
		Value:      e.Value,
		Confidence: e.Confidence,
		CreatedAt:  e.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  e.UpdatedAt.Format(time.RFC3339),
	}
	if e.SourceAgent != nil {
		info.SourceAgent = *e.SourceAgent
	}
	if e.SourceRef != nil {
		info.SourceRef = *e.SourceRef
	}
	return info
}

func isValidBlackboardEntryType(t store.BlackboardEntryType) bool {
	switch t {
	case store.BlackboardTypeDecision,
		store.BlackboardTypeFinding,
		store.BlackboardTypeAttempt,
		store.BlackboardTypeQuestion,
		store.BlackboardTypeArtifact:
		return true
	}
	return false
}

func isValidStateEntryType(t store.StateEntryType) bool {
	switch t {
	case store.StateTypeArchitecture,
		store.StateTypeConvention,
		store.StateTypeConstraint,
		store.StateTypeDecision,
		store.StateTypeEnvironment:
		return true
	}
	return false
}

// handleSubscribeStream enables stream mode for a client.
// The client will receive StreamEvents matching their filter criteria.
func (d *Daemon) handleSubscribeStream(
	params json.RawMessage,
	enableStream func(filter *control.SubscribeStreamRequest),
) (any, error) {
	var req control.SubscribeStreamRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
	}

	// Enable stream mode with the filter
	enableStream(&req)

	// Log subscription
	logging.Info("stream subscriber connected",
		"agent_filter", req.AgentID,
		"worktree_filter", req.WorktreePath,
		"event_types", req.EventTypes)

	// Return subscription confirmation
	return map[string]any{
		"subscribed":        true,
		"filter":            req,
		"active_agents":     d.countActiveAgents(),
		"stream_subscribers": d.server.StreamSubscriberCount(),
	}, nil
}

// countActiveAgents returns the number of running agents.
func (d *Daemon) countActiveAgents() int {
	agents, err := d.store.ListRunningAgents()
	if err != nil {
		return 0
	}
	return len(agents)
}

// EmitStreamEvent broadcasts a StreamEvent to all stream subscribers.
// This is the main entry point for emitting events from the daemon.
func (d *Daemon) EmitStreamEvent(event *control.StreamEvent) {
	logging.Debug("emitting stream event",
		"type", event.Type,
		"agent_id", event.AgentID,
		"subscribers", d.server.StreamSubscriberCount())
	d.server.BroadcastStreamEvent(event)
}

// emitAgentStreamEvent converts agent event data to a StreamEvent and broadcasts it.
// This is called by the spawner callback when agent activity events occur.
func (d *Daemon) emitAgentStreamEvent(eventType, agentID, worktreePath string, payload any) {
	// Map string event type to StreamEventType
	var streamType control.StreamEventType
	switch eventType {
	case "tool_call":
		streamType = control.StreamEventToolCall
	case "tool_result":
		streamType = control.StreamEventToolResult
	case "thinking":
		streamType = control.StreamEventThinking
	case "message":
		streamType = control.StreamEventMessage
	case "agent_crashed":
		streamType = control.StreamEventAgentCrashed
	case "agent_terminated":
		streamType = control.StreamEventAgentTerminated
	default:
		streamType = control.StreamEventMessage
	}

	event := control.NewStreamEvent(streamType, control.StreamSourceAgent).
		WithAgent(agentID).
		WithWorktree(worktreePath).
		WithPayload(payload)

	d.EmitStreamEvent(event)
}
