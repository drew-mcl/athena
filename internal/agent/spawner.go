// Package agent handles Claude Code agent lifecycle management.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/drewfead/athena/internal/config"
	agentctx "github.com/drewfead/athena/internal/context"
	"github.com/drewfead/athena/internal/data"
	"github.com/drewfead/athena/internal/eventlog"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/runner"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/worktree"
	"github.com/google/uuid"
)

// StreamEventEmitter is a callback for emitting stream events to visualizers.
// The callback receives event type, agent ID, worktree path, and payload.
type StreamEventEmitter func(eventType, agentID, worktreePath string, payload any)

// Spawner manages the creation and tracking of Claude Code agent processes.
type Spawner struct {
	config     *config.Config
	store      *store.Store
	pipeline   *eventlog.Pipeline
	contextMgr *agentctx.Manager
	publisher  *worktree.Publisher // For auto-PR publishing

	processes    map[string]*ManagedProcess
	mu           sync.RWMutex
	streamEmitter StreamEventEmitter
}

// ManagedProcess wraps a Claude process with management metadata.
type ManagedProcess struct {
	AgentID   string
	SessionID string
	Process   runner.Session
	Cancel    context.CancelFunc
}

// NewSpawner creates a new agent spawner with default pipeline.
func NewSpawner(cfg *config.Config, st *store.Store, pub *worktree.Publisher) *Spawner {
	return NewSpawnerWithPipeline(cfg, st, pub, eventlog.NewSQLitePipeline(st, 100))
}

// NewSpawnerWithPipeline creates a spawner with a custom pipeline.
func NewSpawnerWithPipeline(cfg *config.Config, st *store.Store, pub *worktree.Publisher, pipeline *eventlog.Pipeline) *Spawner {
	return &Spawner{
		config:     cfg,
		store:      st,
		pipeline:   pipeline,
		contextMgr: agentctx.NewManager(st),
		publisher:  pub,
		processes:  make(map[string]*ManagedProcess),
	}
}

// Pipeline returns the event pipeline for external subscriptions.
func (s *Spawner) Pipeline() *eventlog.Pipeline {
	return s.pipeline
}

// ContextManager returns the context manager for external access.
func (s *Spawner) ContextManager() *agentctx.Manager {
	return s.contextMgr
}

// SetStreamEmitter sets the callback for emitting stream events.
func (s *Spawner) SetStreamEmitter(emitter StreamEventEmitter) {
	s.streamEmitter = emitter
}

// SpawnSpec defines how to spawn an agent.
type SpawnSpec struct {
	WorktreePath string
	ProjectName  string
	Archetype    string
	Prompt       string
	ParentID     string
	Provider     string
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

	// Build run spec based on archetype
	runSpec, provider := s.buildRunSpec(spec, sessionID)

	// Log the spec being executed for debugging
	specBytes, _ := json.Marshal(runSpec)
	cmdPayload := fmt.Sprintf(`{"type":"spawn_spec","spec":%s,"provider":"%s"}`, string(specBytes), provider)
	s.store.LogAgentEvent(agentID, "spawn_spec", cmdPayload)
	logging.Debug("spawning agent", "agent_id", agentID, "provider", provider, "workdir", runSpec.WorkDir)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)

	// Initialize runner
	r, err := runner.New(provider)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create runner for provider %s: %w", provider, err)
	}

	// Spawn the session
	session, err := r.Start(procCtx, runSpec)
	if err != nil {
		cancel()
		s.store.UpdateAgentStatus(agentID, store.AgentStatusCrashed)
		// Log the spawn failure for visibility in the TUI
		errPayload := fmt.Sprintf(`{"type":"spawn_failed","message":%q,"provider":"%s"}`, err.Error(), provider)
		s.store.LogAgentEvent(agentID, "spawn_failed", errPayload)
		logging.Error("failed to spawn agent",
			"agent_id", agentID,
			"provider", provider,
			"workdir", runSpec.WorkDir,
			"archetype", spec.Archetype,
			"error", err)
		return nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Update agent with PID
	pid := session.PID()
	s.store.UpdateAgentPID(agentID, pid)
	s.store.UpdateAgentStatus(agentID, store.AgentStatusRunning)

	// Track the managed process
	mp := &ManagedProcess{
		AgentID:   agentID,
		SessionID: sessionID,
		Process:   session,
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
	mp.Process.Stop()
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

func (s *Spawner) buildRunSpec(spec SpawnSpec, sessionID string) (runner.RunSpec, string) {
	archetype := s.config.Archetypes[spec.Archetype]

	// Build prompt with context prepended
	prompt := spec.Prompt
	if s.contextMgr != nil {
		promptWithContext, err := s.contextMgr.BuildPromptWithContext(spec.Prompt, spec.WorktreePath, spec.ProjectName)
		if err != nil {
			logging.Warn("failed to build context for prompt", "error", err, "worktree", spec.WorktreePath)
		} else {
			prompt = promptWithContext
		}
	}

	runSpec := runner.RunSpec{
		SessionID:       sessionID,
		WorkDir:         spec.WorktreePath,
		Prompt:          prompt,
		Model:           archetype.Model,
		PermissionMode:  archetype.PermissionMode,
		AllowedTools:    archetype.AllowedTools,
		SystemPrompt:    archetype.Prompt,
		GitIdentity:     s.resolveGitIdentity(spec.Archetype),
		Env:             make(map[string]string),
	}

	// Populate environment from config
	if s.config.Gemini.APIKey != "" {
		runSpec.Env["GEMINI_API_KEY"] = s.config.Gemini.APIKey
	}

	// Use defaults if archetype not found
	if archetype.Model == "" {
		runSpec.Model = s.config.Agents.Model
	}

	provider := spec.Provider
	if provider == "" {
		provider = archetype.Provider
	}
	if provider == "" {
		provider = s.config.Agents.Provider
	}
	if provider == "" {
		provider = "claude" // Default fallback
	}

	return runSpec, provider
}

// resolveGitIdentity returns the git identity config for a given archetype.
// It first checks for archetype-specific identity, then falls back to default.
// Returns nil if no identity is configured (graceful fallback to local git config).
func (s *Spawner) resolveGitIdentity(archetype string) *runner.GitIdentityConfig {
	identities := s.config.Integrations.Identities

	// Try archetype-specific identity first
	var identity *config.AgentIdentity
	if identities.Archetypes != nil {
		identity = identities.Archetypes[archetype]
	}

	// Fall back to default identity
	if identity == nil {
		identity = identities.Default
	}

	// No identity configured - graceful fallback
	if identity == nil || identity.Name == "" {
		return nil
	}

	gitConfig := &runner.GitIdentityConfig{
		AuthorName:  identity.Name,
		AuthorEmail: identity.Email,
	}

	// Add co-author line if configured
	if identities.CoAuthor != nil {
		gitConfig.CoAuthorLine = identities.CoAuthor.CoAuthorLine()
	}

	return gitConfig
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
			// Store the error in agent_events so it's visible in the TUI
			errPayload := fmt.Sprintf(`{"type":"error","message":%q}`, err.Error())
			s.store.LogAgentEvent(mp.AgentID, "error", errPayload)

			// Emit stream event for errors
			if s.streamEmitter != nil {
				worktreePath := ""
				if agent, _ := s.store.GetAgent(mp.AgentID); agent != nil {
					worktreePath = agent.WorktreePath
				}
				s.streamEmitter("agent_crashed", mp.AgentID, worktreePath, map[string]any{
					"type":    "error",
					"message": err.Error(),
				})
			}

		case <-mp.Process.Done():
			s.handleExit(mp)
			return
		}
	}
}

func (s *Spawner) processEvent(mp *ManagedProcess, event runner.Event) {
	// Convert to unified message and ingest through pipeline
	msg := data.FromRunnerEvent(mp.AgentID, 0, event) // Sequence assigned by pipeline
	msg.SessionID = mp.SessionID

	ctx := context.Background()
	if err := s.pipeline.Ingest(ctx, msg); err != nil {
		logging.Error("failed to ingest event", "agent_id", mp.AgentID, "error", err)
	}

	// Log to agent_events with full content for TUI visibility
	// Re-serialize the event raw input if needed, or construct payload
	payload := string(event.Raw)
	if payload == "" || payload == "null" {
		// Construct payload if raw is missing
		payload = fmt.Sprintf(`{"type":"%s","subtype":"%s","content":%q}`, event.Type, event.Subtype, event.Content)
	}
	s.store.LogAgentEvent(mp.AgentID, event.Type, payload)

	// Emit stream event for visualization
	if s.streamEmitter != nil {
		// Get worktree path for event context
		worktreePath := ""
		if agent, err := s.store.GetAgent(mp.AgentID); err == nil && agent != nil {
			worktreePath = agent.WorktreePath
		}

		// Map runner event types to stream event types
		streamEventType := s.mapEventType(event.Type, event.Subtype)
		streamPayload := map[string]any{
			"type":    event.Type,
			"subtype": event.Subtype,
			"content": truncateContent(event.Content, 500),
		}

		// Add tool-specific fields if present
		if event.Name != "" {
			streamPayload["tool_name"] = event.Name
		}

		logging.Debug("spawner emitting stream event",
			"event_type", streamEventType,
			"runner_type", event.Type,
			"agent_id", mp.AgentID)
		s.streamEmitter(streamEventType, mp.AgentID, worktreePath, streamPayload)
	} else {
		logging.Debug("spawner stream emitter not set", "agent_id", mp.AgentID)
	}

	// Extract structured markers from assistant messages
	if s.contextMgr != nil && event.Type == "assistant" && event.Content != "" {
		s.extractMarkersFromContent(mp, event.Content)
	}

	// Update status based on event type
	// runner.Event doesn't have IsThinking helper, check types
	switch {
	case event.Subtype == "thinking":
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusPlanning)

	case event.Type == "tool_use":
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusExecuting)

	case event.Type == "result":
		if event.Subtype != "error" {
			s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCompleted)
		} else {
			s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCrashed)
		}
	}

	// Update heartbeat
	s.store.UpdateHeartbeat(mp.AgentID)
}

// extractMarkersFromContent parses structured markers from agent output and records them.
func (s *Spawner) extractMarkersFromContent(mp *ManagedProcess, content string) {
	// Get agent info for worktree/project context
	agent, err := s.store.GetAgent(mp.AgentID)
	if err != nil || agent == nil {
		logging.Debug("could not get agent for marker extraction", "agent_id", mp.AgentID)
		return
	}

	// Parse and record markers
	markers, err := s.contextMgr.ParseAgentOutput(agent.WorktreePath, agent.ProjectName, mp.AgentID, content)
	if err != nil {
		logging.Warn("failed to parse agent output for markers", "agent_id", mp.AgentID, "error", err)
		return
	}

	if len(markers) > 0 {
		logging.Debug("extracted markers from agent output",
			"agent_id", mp.AgentID,
			"count", len(markers),
			"worktree", agent.WorktreePath)
	}
}

func (s *Spawner) handleExit(mp *ManagedProcess) {
	s.mu.Lock()
	delete(s.processes, mp.AgentID)
	s.mu.Unlock()

	exitCode := mp.Process.ExitCode()
	s.store.UpdateAgentExitCode(mp.AgentID, exitCode)

	// Log the exit event with details
	exitPayload := fmt.Sprintf(`{"type":"exit","exit_code":%d}`, exitCode)
	s.store.LogAgentEvent(mp.AgentID, "exit", exitPayload)

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
		logging.Info("agent completed successfully", "agent_id", mp.AgentID)

		// Check for automatic workflow: auto-approve plan and spawn executor
		s.maybeAutoSpawnExecutor(agent)

		// Check for automatic workflow: auto-publish PR on executor completion
		s.maybeAutoPublishPR(agent)
	} else {
		s.store.UpdateAgentStatus(mp.AgentID, store.AgentStatusCrashed)
		logging.Warn("agent crashed", "agent_id", mp.AgentID, "exit_code", exitCode)
		// Log crash event for visibility
		crashPayload := fmt.Sprintf(`{"type":"crashed","exit_code":%d,"message":"Process exited with non-zero code"}`, exitCode)
		s.store.LogAgentEvent(mp.AgentID, "crashed", crashPayload)
	}
}

// maybeAutoSpawnExecutor checks if automatic workflow mode should trigger executor spawn.
func (s *Spawner) maybeAutoSpawnExecutor(agent *store.Agent) {
	// Only auto-spawn for planner agents
	if agent.Archetype != "planner" {
		return
	}

	// Get the worktree to check its workflow mode
	wt, err := s.store.GetWorktree(agent.WorktreePath)
	if err != nil || wt == nil {
		logging.Debug("no worktree found for auto-executor check", "path", agent.WorktreePath)
		return
	}

	// Check if workflow mode is automatic
	if wt.WorkflowMode == nil || *wt.WorkflowMode != string(config.WorkflowModeAutomatic) {
		return
	}

	// Get the plan
	plan, err := s.store.GetPlan(agent.WorktreePath)
	if err != nil || plan == nil {
		logging.Debug("no plan found for auto-executor", "path", agent.WorktreePath)
		return
	}

	// Only proceed if plan is in draft status
	if plan.Status != store.PlanStatusDraft {
		logging.Debug("plan not in draft status, skipping auto-executor",
			"path", agent.WorktreePath, "status", plan.Status)
		return
	}

	// Get plan content - try DB cache first, then Claude's native storage
	planContent := plan.Content
	if planContent == "" && agent.ClaudeSessionID != "" {
		// Fall back to reading Claude's native plan storage
		content, err := readClaudePlan(agent.WorktreePath, agent.ClaudeSessionID)
		if err != nil {
			logging.Warn("could not read plan from Claude storage", "error", err, "worktree", agent.WorktreePath)
		} else {
			planContent = content
			// Cache it for future use
			s.store.UpdatePlanContent(agent.WorktreePath, content)
		}
	}

	if planContent == "" {
		logging.Error("cannot auto-spawn executor: no plan content found",
			"worktree", agent.WorktreePath, "plan_id", plan.ID)
		return
	}

	logging.Info("automatic mode: auto-approving plan and spawning executor",
		"worktree", agent.WorktreePath,
		"plan_id", plan.ID)

	// Approve the plan
	if err := s.store.UpdatePlanStatus(agent.WorktreePath, store.PlanStatusApproved); err != nil {
		logging.Error("failed to auto-approve plan", "error", err)
		return
	}

	// Build executor prompt with embedded plan content
	prompt := fmt.Sprintf(`## Approved Implementation Plan

%s

---

Execute this plan precisely. After each step, report what you did.`, planContent)

	// Spawn executor agent
	spec := SpawnSpec{
		WorktreePath: agent.WorktreePath,
		ProjectName:  agent.ProjectName,
		Archetype:    "executor",
		Prompt:       prompt,
		ParentID:     agent.ID,
	}

	ctx := context.Background()
	_, err = s.Spawn(ctx, spec)
	if err != nil {
		logging.Error("failed to auto-spawn executor", "error", err, "worktree", agent.WorktreePath)
		// Revert plan status on failure
		s.store.UpdatePlanStatus(agent.WorktreePath, store.PlanStatusDraft)
		return
	}

	// Update plan to executing status
	s.store.UpdatePlanStatus(agent.WorktreePath, store.PlanStatusExecuting)
}

// maybeAutoPublishPR checks if automatic workflow mode should publish a PR on executor completion.
func (s *Spawner) maybeAutoPublishPR(agent *store.Agent) {
	// Only auto-publish for executor agents
	if agent.Archetype != "executor" {
		return
	}

	// Check GitHub auto-PR is enabled
	gh := s.config.Integrations.GitHub
	if !gh.Enabled || !gh.AutoPR {
		logging.Debug("GitHub auto-PR not enabled, skipping auto-publish")
		return
	}

	// Get the worktree
	wt, err := s.store.GetWorktree(agent.WorktreePath)
	if err != nil || wt == nil {
		logging.Debug("no worktree found for auto-publish", "path", agent.WorktreePath)
		return
	}

	// Check workflow mode is automatic
	if wt.WorkflowMode == nil || *wt.WorkflowMode != string(config.WorkflowModeAutomatic) {
		return
	}

	// Skip if already published
	if wt.PRURL != nil && *wt.PRURL != "" {
		logging.Debug("worktree already has PR, skipping auto-publish", "path", agent.WorktreePath)
		return
	}

	// Skip if publisher not available
	if s.publisher == nil {
		logging.Debug("publisher not available for auto-publish")
		return
	}

	logging.Info("automatic mode: auto-publishing PR",
		"worktree", agent.WorktreePath)

	// Publish the PR
	result, err := s.publisher.PublishPR(worktree.PublishOptions{
		WorktreePath: agent.WorktreePath,
		Archetype:    agent.Archetype,
	})
	if err != nil {
		logging.Error("failed to auto-publish PR", "error", err, "worktree", agent.WorktreePath)
		return
	}

	logging.Info("PR auto-published successfully",
		"url", result.PRURL,
		"branch", result.Branch,
		"worktree", agent.WorktreePath)
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

// mapEventType converts runner event types to stream event type strings.
func (s *Spawner) mapEventType(eventType, subtype string) string {
	switch eventType {
	case "tool_use":
		return "tool_call"
	case "tool_result":
		return "tool_result"
	case "assistant":
		if subtype == "thinking" {
			return "thinking"
		}
		return "message"
	case "result":
		if subtype == "error" {
			return "agent_crashed"
		}
		return "agent_terminated"
	default:
		return "message"
	}
}

// truncateContent limits content length for stream payloads.
func truncateContent(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
