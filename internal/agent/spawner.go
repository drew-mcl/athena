// Package agent handles Claude Code agent lifecycle management.
package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/data"
	"github.com/drewfead/athena/internal/eventlog"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/pkg/claudecode"
	"github.com/google/uuid"
)

// Spawner manages the creation and tracking of Claude Code agent processes.
type Spawner struct {
	config   *config.Config
	store    *store.Store
	pipeline *eventlog.Pipeline

	processes map[string]*ManagedProcess
	mu        sync.RWMutex
}

// ManagedProcess wraps a Claude process with management metadata.
type ManagedProcess struct {
	AgentID   string
	SessionID string
	Process   *claudecode.Process
	Cancel    context.CancelFunc
}

// NewSpawner creates a new agent spawner with default pipeline.
func NewSpawner(cfg *config.Config, st *store.Store) *Spawner {
	return NewSpawnerWithPipeline(cfg, st, eventlog.NewSQLitePipeline(st, 100))
}

// NewSpawnerWithPipeline creates a spawner with a custom pipeline.
func NewSpawnerWithPipeline(cfg *config.Config, st *store.Store, pipeline *eventlog.Pipeline) *Spawner {
	return &Spawner{
		config:    cfg,
		store:     st,
		pipeline:  pipeline,
		processes: make(map[string]*ManagedProcess),
	}
}

// Pipeline returns the event pipeline for external subscriptions.
func (s *Spawner) Pipeline() *eventlog.Pipeline {
	return s.pipeline
}

// SpawnSpec defines how to spawn an agent.
type SpawnSpec struct {
	WorktreePath string
	ProjectName  string
	Archetype    string
	Prompt       string
	ParentID     string
}

// Spawn creates and starts a new Claude Code agent.
func (s *Spawner) Spawn(ctx context.Context, spec SpawnSpec) (*store.Agent, error) {
	// Generate IDs
	agentID := uuid.NewString()
	sessionID := uuid.NewString()

	// Create agent record
	agent := &store.Agent{
		ID:              agentID,
		WorktreePath:    spec.WorktreePath,
		ProjectName:     spec.ProjectName,
		Archetype:       spec.Archetype,
		Status:          store.AgentStatusSpawning,
		Prompt:          spec.Prompt,
		ClaudeSessionID: sessionID,
	}
	if spec.ParentID != "" {
		agent.ParentAgentID = &spec.ParentID
	}

	if err := s.store.CreateAgent(agent); err != nil {
		return nil, fmt.Errorf("failed to create agent record: %w", err)
	}

	// Build spawn options based on archetype
	opts := s.buildOptions(spec, sessionID)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)

	// Spawn the process
	proc, err := claudecode.Spawn(procCtx, opts)
	if err != nil {
		cancel()
		s.store.UpdateAgentStatus(agentID, store.AgentStatusCrashed)
		return nil, fmt.Errorf("failed to spawn claude: %w", err)
	}

	// Update agent with PID
	pid := proc.PID()
	s.store.UpdateAgentPID(agentID, pid)
	s.store.UpdateAgentStatus(agentID, store.AgentStatusRunning)

	// Track the managed process
	mp := &ManagedProcess{
		AgentID:   agentID,
		SessionID: sessionID,
		Process:   proc,
		Cancel:    cancel,
	}

	s.mu.Lock()
	s.processes[agentID] = mp
	s.mu.Unlock()

	// Record initial prompt in data plane
	promptMsg := data.NewPromptMessage(uuid.NewString(), agentID, sessionID, spec.Prompt, 0)
	if err := s.pipeline.Ingest(ctx, promptMsg); err != nil {
		logging.Error("failed to record prompt", "agent_id", agentID, "error", err)
	}

	// Start event handler
	go s.handleEvents(mp)

	return agent, nil
}

// Kill terminates an agent by ID.
func (s *Spawner) Kill(agentID string) error {
	s.mu.Lock()
	mp, ok := s.processes[agentID]
	if ok {
		delete(s.processes, agentID)
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent not running: %s", agentID)
	}

	mp.Cancel()
	mp.Process.Kill()
	s.store.UpdateAgentStatus(agentID, store.AgentStatusTerminated)

	return nil
}

// GetProcess returns the managed process for an agent.
func (s *Spawner) GetProcess(agentID string) (*ManagedProcess, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mp, ok := s.processes[agentID]
	return mp, ok
}

// ListRunning returns all currently running agent IDs.
func (s *Spawner) ListRunning() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.processes))
	for id := range s.processes {
		ids = append(ids, id)
	}
	return ids
}

func (s *Spawner) buildOptions(spec SpawnSpec, sessionID string) *claudecode.SpawnOptions {
	archetype := s.config.Archetypes[spec.Archetype]

	opts := &claudecode.SpawnOptions{
		SessionID:      sessionID,
		WorkDir:        spec.WorktreePath,
		Prompt:         spec.Prompt,
		Model:          archetype.Model,
		PermissionMode: archetype.PermissionMode,
		AllowedTools:   archetype.AllowedTools,
		SystemPrompt:   archetype.Prompt,
	}

	// Use defaults if archetype not found
	if archetype.Model == "" {
		opts.Model = s.config.Agents.Model
	}

	return opts
}

func (s *Spawner) handleEvents(mp *ManagedProcess) {
	for {
		select {
		case event, ok := <-mp.Process.Events():
			if !ok {
				return
			}
			s.processEvent(mp, event)

		case err, ok := <-mp.Process.Errors():
			if !ok {
				return
			}
			logging.Error("agent process error", "agent_id", mp.AgentID, "error", err)

		case <-mp.Process.Done():
			s.handleExit(mp)
			return
		}
	}
}

func (s *Spawner) processEvent(mp *ManagedProcess, event *claudecode.Event) {
	// Convert to unified message and ingest through pipeline
	msg := data.FromClaudeEvent(mp.AgentID, 0, event) // Sequence assigned by pipeline
	msg.SessionID = mp.SessionID

	ctx := context.Background()
	if err := s.pipeline.Ingest(ctx, msg); err != nil {
		logging.Error("failed to ingest event", "agent_id", mp.AgentID, "error", err)
	}

	// Legacy: also log to agent_events for backwards compat
	payload := fmt.Sprintf(`{"type":"%s","subtype":"%s"}`, event.Type, event.Subtype)
	s.store.LogAgentEvent(mp.AgentID, string(event.Type), payload)

	// Update status based on event type
	switch {
	case event.IsThinking():
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusPlanning)

	case event.IsToolUse():
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusExecuting)

	case event.IsComplete():
		if event.IsSuccess() {
			s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCompleted)
		} else {
			s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCrashed)
		}
	}

	// Update heartbeat
	s.store.UpdateHeartbeat(mp.AgentID)
}

func (s *Spawner) handleExit(mp *ManagedProcess) {
	s.mu.Lock()
	delete(s.processes, mp.AgentID)
	s.mu.Unlock()

	exitCode := mp.Process.ExitCode()
	s.store.UpdateAgentExitCode(mp.AgentID, exitCode)

	// Get current agent state
	agent, _ := s.store.GetAgent(mp.AgentID)
	if agent == nil {
		return
	}

	// If not already in a terminal state, mark based on exit code
	switch agent.Status {
	case store.AgentStatusCompleted, store.AgentStatusTerminated:
		// Already in terminal state
		return
	}

	if exitCode == 0 {
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCompleted)
	} else {
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCrashed)
	}
}
