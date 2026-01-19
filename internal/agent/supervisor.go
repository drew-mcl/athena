package agent

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/runner"
	"github.com/drewfead/athena/internal/store"
)

// Supervisor monitors agent health and handles crash recovery.
type Supervisor struct {
	config  *config.Config
	store   *store.Store
	spawner *Spawner

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSupervisor creates a new agent supervisor.
func NewSupervisor(cfg *config.Config, st *store.Store, sp *Spawner) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{
		config:  cfg,
		store:   st,
		spawner: sp,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins supervisor monitoring loops.
func (s *Supervisor) Start() {
	s.wg.Add(2)
	go s.healthCheckLoop()
	go s.crashRecoveryLoop()
}

// Stop terminates the supervisor.
func (s *Supervisor) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *Supervisor) healthCheckLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.Agents.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkHealth()
		}
	}
}

func (s *Supervisor) checkHealth() {
	agents, err := s.store.ListRunningAgents()
	if err != nil {
		logging.Error("supervisor failed to list agents", "error", err)
		return
	}

	for _, agent := range agents {
		// Check if we have a process for this agent
		mp, ok := s.spawner.GetProcess(agent.ID)
		if !ok {
			// Process not tracked - might be orphaned from previous run
			s.handleOrphanedAgent(agent)
			continue
		}

		// Check if process is still running
		if !mp.Process.IsRunning() {
			continue // Will be handled by exit handler
		}

		// Check heartbeat timeout
		if agent.LastHeartbeat != nil {
			since := time.Since(*agent.LastHeartbeat)
			if since > s.config.Agents.HeartbeatTimeout {
				logging.Warn("agent heartbeat timeout", "agent_id", agent.ID, "since", since)
				s.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
			}
		}
	}
}

func (s *Supervisor) handleOrphanedAgent(agent *store.Agent) {
	// Check if the PID is still running
	if agent.PID != nil && processExists(*agent.PID) {
		logging.Warn("agent has orphaned process", "agent_id", agent.ID, "pid", *agent.PID)
		// For now, just mark as crashed - could try to reattach later
	}

	// If process isn't running, mark as crashed
	s.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
}

func (s *Supervisor) crashRecoveryLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.recoverCrashedAgents()
		}
	}
}

func (s *Supervisor) recoverCrashedAgents() {
	agents, err := s.store.ListAgents(store.AgentStatusCrashed)
	if err != nil {
		return
	}

	for _, agent := range agents {
		if s.shouldRestart(agent) {
			s.restartAgent(agent)
		}
	}
}

func (s *Supervisor) shouldRestart(agent *store.Agent) bool {
	// Check restart policy
	policy := s.config.Agents.RestartPolicy
	if policy == "never" {
		return false
	}

	// Check restart count
	if agent.RestartCount >= s.config.Agents.MaxRestarts {
		logging.Warn("agent exceeded max restarts", "agent_id", agent.ID, "max_restarts", s.config.Agents.MaxRestarts)
		return false
	}

	// "on-failure" policy only restarts non-zero exits
	if policy == "on-failure" {
		if agent.ExitCode != nil && *agent.ExitCode == 0 {
			return false
		}
	}

	return true
}

func (s *Supervisor) restartAgent(agent *store.Agent) {
	logging.Info("restarting agent", "agent_id", agent.ID, "attempt", agent.RestartCount+1)

	// Increment restart count
	s.store.IncrementRestartCount(agent.ID)
	s.store.UpdateAgentStatus(agent.ID, store.AgentStatusPending)

	// Calculate backoff
	backoff := s.calculateBackoff(agent.RestartCount)
	time.Sleep(backoff)

	// Resume using the existing session
	opts := runner.ResumeSpec{
		SessionID: agent.ClaudeSessionID,
		WorkDir:   agent.WorktreePath,
		Model:     s.config.Agents.Model,
	}

	if err := s.spawner.Resume(s.ctx, agent.ID, opts); err != nil {
		logging.Error("failed to restart agent", "agent_id", agent.ID, "error", err)
		s.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
	}
}

func (s *Supervisor) calculateBackoff(restartCount int) time.Duration {
	cfg := s.config.Agents.RestartBackoff
	backoff := cfg.Initial

	for i := 0; i < restartCount; i++ {
		backoff = time.Duration(float64(backoff) * cfg.Multiplier)
		if backoff > cfg.Max {
			backoff = cfg.Max
			break
		}
	}

	return backoff
}

// processExists checks if a process with the given PID is running.
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// Resume restarts an agent using the runner abstraction.
func (s *Spawner) Resume(ctx context.Context, agentID string, spec runner.ResumeSpec) error {
	agent, err := s.store.GetAgent(agentID)
	if err != nil || agent == nil {
		return err
	}

	// Initialize runner
	// We need to know the provider. In restart case, we assume same provider as config or infer?
	// The agent record doesn't store provider explicitly unless we added it.
	// We added Provider to config. But if agent was spawned with a specific provider (e.g. from archetype), we might lose it if we don't store it.
	// For now, use config default.
	provider := s.config.Agents.Provider
	if provider == "" {
		provider = "claude"
	}

	r, err := runner.New(provider)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	procCtx, cancel := context.WithCancel(ctx)

	session, err := r.Resume(procCtx, spec)
	if err != nil {
		cancel()
		return err
	}

	pid := session.PID()
	s.store.UpdateAgentPID(agentID, pid)
	s.store.UpdateAgentStatus(agentID, store.AgentStatusRunning)

	mp := &ManagedProcess{
		AgentID:   agentID,
		SessionID: spec.SessionID,
		Process:   session,
		Cancel:    cancel,
	}

	s.mu.Lock()
	s.processes[agentID] = mp
	s.mu.Unlock()

	go s.handleEvents(mp)

	return nil
}
