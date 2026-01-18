// Package daemon implements the athenad background service.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/worktree"
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
	spawner     *agent.Spawner
	executor    *JobExecutor

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

	d := &Daemon{
		config:      cfg,
		store:       st,
		server:      control.NewServer(cfg.Daemon.Socket),
		scanner:     worktree.NewScanner(cfg, st),
		provisioner: worktree.NewProvisioner(cfg, st),
		migrator:    worktree.NewMigrator(cfg, st),
		spawner:     agent.NewSpawner(cfg, st),
		agents:      make(map[string]*AgentProcess),
		jobQueue:    make(chan string, 100),
		ctx:         ctx,
		cancel:      cancel,
	}
	d.executor = NewJobExecutor(d)

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
}

func (d *Daemon) handleListAgents(_ json.RawMessage) (any, error) {
	agents, err := d.store.ListAgents()
	if err != nil {
		return nil, err
	}

	var result []*control.AgentInfo
	for _, a := range agents {
		result = append(result, agentToInfo(a))
	}
	return result, nil
}

func (d *Daemon) handleGetAgent(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	agent, err := d.store.GetAgent(req.ID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", req.ID)
	}

	return agentToInfo(agent), nil
}

func (d *Daemon) handleGetAgentLogs(params json.RawMessage) (any, error) {
	var req struct {
		AgentID string `json:"agent_id"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if req.Limit <= 0 {
		req.Limit = 100 // Default limit
	}

	events, err := d.store.GetAgentEvents(req.AgentID, req.Limit)
	if err != nil {
		return nil, err
	}

	var result []*control.AgentEventInfo
	for _, e := range events {
		result = append(result, &control.AgentEventInfo{
			ID:        e.ID,
			AgentID:   e.AgentID,
			EventType: e.EventType,
			Payload:   e.Payload,
			Timestamp: e.Timestamp.Format(time.RFC3339),
		})
	}
	return result, nil
}

func (d *Daemon) handleSpawnAgent(params json.RawMessage) (any, error) {
	var req control.SpawnAgentRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Validate worktree exists
	wt, err := d.store.GetWorktree(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	if wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", req.WorktreePath)
	}

	// Build spawn spec
	spec := agent.SpawnSpec{
		WorktreePath: req.WorktreePath,
		ProjectName:  wt.Project,
		Archetype:    req.Archetype,
		Prompt:       req.Prompt,
	}

	// Actually spawn the agent process
	spawnedAgent, err := d.spawner.Spawn(d.ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Associate agent with worktree
	d.store.AssignAgentToWorktree(req.WorktreePath, spawnedAgent.ID)

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: agentToInfo(spawnedAgent),
	})

	return agentToInfo(spawnedAgent), nil
}

func (d *Daemon) handleKillAgent(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	agentRecord, err := d.store.GetAgent(req.ID)
	if err != nil {
		return nil, err
	}
	if agentRecord == nil {
		return nil, fmt.Errorf("agent not found: %s", req.ID)
	}

	// Kill via spawner (handles process cleanup)
	if err := d.spawner.Kill(req.ID); err != nil {
		logging.Debug("spawner kill returned error (may not be running)", "agent_id", req.ID, "error", err)
	}

	// Also try the legacy agent map
	d.agentsMu.Lock()
	if proc, ok := d.agents[req.ID]; ok {
		proc.Cancel()
		delete(d.agents, req.ID)
	}
	d.agentsMu.Unlock()

	// Update status
	d.store.UpdateAgentStatus(req.ID, store.AgentStatusTerminated)
	d.store.ClearWorktreeAgent(agentRecord.WorktreePath)

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "agent_terminated",
		Payload: map[string]string{"id": req.ID},
	})

	return map[string]bool{"success": true}, nil
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

func (d *Daemon) handleListJobs(_ json.RawMessage) (any, error) {
	jobs, err := d.store.ListJobs()
	if err != nil {
		return nil, err
	}

	var result []*control.JobInfo
	for _, j := range jobs {
		result = append(result, jobToInfo(j))
	}
	return result, nil
}

func jobToInfo(j *store.Job) *control.JobInfo {
	info := &control.JobInfo{
		ID:              j.ID,
		RawInput:        j.RawInput,
		NormalizedInput: j.NormalizedInput,
		Status:          string(j.Status),
		Type:            string(j.Type),
		Project:         j.Project,
		CreatedAt:       j.CreatedAt.Format(time.RFC3339),
	}
	if j.CurrentAgentID != nil {
		info.AgentID = *j.CurrentAgentID
	}
	if j.ExternalID != nil {
		info.ExternalID = *j.ExternalID
	}
	if j.ExternalURL != nil {
		info.ExternalURL = *j.ExternalURL
	}
	if j.Answer != nil {
		info.Answer = *j.Answer
	}
	if j.WorktreePath != nil {
		info.WorktreePath = *j.WorktreePath
	}
	return info
}

func (d *Daemon) handleCreateJob(params json.RawMessage) (any, error) {
	// Reject new jobs if we're draining
	if d.isDraining() {
		return nil, fmt.Errorf("daemon is shutting down, not accepting new jobs")
	}

	var req control.CreateJobRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Determine job type (default to feature)
	jobType := store.JobType(req.Type)
	if jobType == "" {
		jobType = store.JobTypeFeature
	}

	// TODO: Normalize input with Haiku (for now, just use as-is)
	job := &store.Job{
		ID:              generateID(),
		RawInput:        req.Input,
		NormalizedInput: req.Input, // Will be normalized by AI
		Status:          store.JobStatusPending,
		Type:            jobType,
		Project:         req.Project,
	}

	// Set target branch for quick jobs
	if jobType == store.JobTypeQuick && req.TargetBranch != "" {
		job.TargetBranch = &req.TargetBranch
	}

	if err := d.store.CreateJob(job); err != nil {
		return nil, err
	}

	// Queue for execution
	select {
	case d.jobQueue <- job.ID:
		logging.Debug("queued job for execution", "job_id", job.ID)
	default:
		logging.Warn("job queue full, will retry on restart", "job_id", job.ID)
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "job_created",
		Payload: jobToInfo(job),
	})

	return jobToInfo(job), nil
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
				// TODO: Reattach to stdout stream
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

func (d *Daemon) checkAgentHealth() {
	agents, err := d.store.ListRunningAgents()
	if err != nil {
		return
	}

	d.agentsMu.RLock()
	defer d.agentsMu.RUnlock()

	for _, agent := range agents {
		if agent.PID == nil {
			continue
		}

		if !processExists(*agent.PID) {
			d.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
			d.server.Broadcast(control.Event{
				Type:    "agent_crashed",
				Payload: map[string]string{"id": agent.ID},
			})
		}
	}
}

func agentToInfo(a *store.Agent) *control.AgentInfo {
	info := &control.AgentInfo{
		ID:              a.ID,
		WorktreePath:    a.WorktreePath,
		ProjectName:     a.ProjectName,
		Project:         a.ProjectName, // Alias for filtering
		Archetype:       a.Archetype,
		Status:          string(a.Status),
		Prompt:          a.Prompt,
		RestartCount:    a.RestartCount,
		CreatedAt:       a.CreatedAt.Format(time.RFC3339),
		ClaudeSessionID: a.ClaudeSessionID, // For claude --resume
	}
	if a.LinearIssueID != nil {
		info.LinearIssueID = *a.LinearIssueID
	}
	return info
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
3. Create a .plan.md file in the worktree root with:
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
			Payload: agentToInfo(spawnedAgent),
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

	// Check DB cache first (unless force refresh)
	if !req.ForceRefresh {
		plan, err := d.store.GetPlan(req.WorktreePath)
		if err == nil && plan != nil {
			return planToInfo(plan), nil
		}
	}

	// Read from file system
	planPath := req.WorktreePath + "/.plan.md"
	content, err := os.ReadFile(planPath)
	if err != nil {
		return nil, fmt.Errorf("no plan found: %w", err)
	}

	// Find planner agent for this worktree
	agents, _ := d.store.ListAgentsByWorktree(req.WorktreePath)
	var plannerID string
	for _, a := range agents {
		if a.Archetype == "planner" {
			plannerID = a.ID
			break
		}
	}

	if plannerID == "" {
		return nil, fmt.Errorf("no planner agent found for worktree")
	}

	// Cache in DB (upsert)
	existingPlan, _ := d.store.GetPlan(req.WorktreePath)
	if existingPlan != nil {
		// Update existing plan
		d.store.UpdatePlanContent(req.WorktreePath, string(content))
		existingPlan.Content = string(content)
		return planToInfo(existingPlan), nil
	}

	// Create new plan
	plan := &store.Plan{
		ID:           generateID(),
		WorktreePath: req.WorktreePath,
		AgentID:      plannerID,
		Content:      string(content),
		Status:       store.PlanStatusDraft,
	}
	if err := d.store.CreatePlan(plan); err != nil {
		return nil, fmt.Errorf("failed to cache plan: %w", err)
	}

	return planToInfo(plan), nil
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

func (d *Daemon) handleSpawnExecutor(params json.RawMessage) (any, error) {
	var req control.SpawnExecutorRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Get worktree
	wt, err := d.store.GetWorktree(req.WorktreePath)
	if err != nil || wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", req.WorktreePath)
	}

	// Get plan
	plan, err := d.store.GetPlan(req.WorktreePath)
	if err != nil || plan == nil {
		return nil, fmt.Errorf("no plan found for worktree: %s", req.WorktreePath)
	}

	// Find planner agent for parent link
	var plannerID string
	agents, _ := d.store.ListAgentsByWorktree(req.WorktreePath)
	for _, a := range agents {
		if a.Archetype == "planner" {
			plannerID = a.ID
			break
		}
	}

	// Build executor prompt with plan
	prompt := fmt.Sprintf(`## Approved Implementation Plan

%s

---

Execute this plan precisely. After each step, report what you did.`, plan.Content)

	// Spawn executor
	spec := agent.SpawnSpec{
		WorktreePath: req.WorktreePath,
		ProjectName:  wt.Project,
		Archetype:    "executor",
		Prompt:       prompt,
		ParentID:     plannerID,
	}

	spawnedAgent, err := d.spawner.Spawn(d.ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn executor: %w", err)
	}

	// Update plan status
	d.store.UpdatePlanStatus(req.WorktreePath, store.PlanStatusExecuting)

	// Associate agent with worktree
	d.store.AssignAgentToWorktree(req.WorktreePath, spawnedAgent.ID)

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: agentToInfo(spawnedAgent),
	})

	return agentToInfo(spawnedAgent), nil
}

func planToInfo(p *store.Plan) *control.PlanInfo {
	return &control.PlanInfo{
		ID:           p.ID,
		WorktreePath: p.WorktreePath,
		AgentID:      p.AgentID,
		Content:      p.Content,
		Status:       string(p.Status),
		CreatedAt:    p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    p.UpdatedAt.Format(time.RFC3339),
	}
}
