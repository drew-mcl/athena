package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

func (d *Daemon) handleListAgents(_ json.RawMessage) (any, error) {
	agents, err := d.store.ListAgents()
	if err != nil {
		return nil, err
	}

	var result []*control.AgentInfo
	for _, a := range agents {
		result = append(result, d.agentToInfo(a))
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

	return d.agentToInfo(agent), nil
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

	// Validate worktree exists in database
	wt, err := d.store.GetWorktree(req.WorktreePath)
	if err != nil {
		return nil, err
	}
	if wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", req.WorktreePath)
	}

	// Validate worktree exists on disk
	if _, err := os.Stat(req.WorktreePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("worktree path does not exist on disk: %s", req.WorktreePath)
	}

	// Build spawn spec
	spec := agent.SpawnSpec{
		WorktreePath: req.WorktreePath,
		ProjectName:  wt.Project,
		Archetype:    req.Archetype,
		Prompt:       req.Prompt,
		Provider:     req.Provider,
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
		Payload: d.agentToInfo(spawnedAgent),
	})

	// Emit stream event for visualization
	d.EmitStreamEvent(control.NewStreamEvent(control.StreamEventAgentCreated, control.StreamSourceDaemon).
		WithAgent(spawnedAgent.ID).
		WithWorktree(req.WorktreePath).
		WithPayload(map[string]any{
			"archetype": req.Archetype,
			"project":   wt.Project,
		}))

	return d.agentToInfo(spawnedAgent), nil
}

func (d *Daemon) handleKillAgent(params json.RawMessage) (any, error) {
	var req struct {
		ID     string `json:"id"`
		Delete bool   `json:"delete"` // If true, fully delete agent and all data
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

	if req.Delete {
		// Full cascade delete - removes agent and all dependent records
		if err := d.store.DeleteAgentCascade(req.ID); err != nil {
			logging.Warn("cascade delete failed", "agent_id", req.ID, "error", err)
			// Fall back to just marking as terminated
			d.store.UpdateAgentStatus(req.ID, store.AgentStatusTerminated)
		}
	} else {
		// Just update status, keep records
		d.store.UpdateAgentStatus(req.ID, store.AgentStatusTerminated)
		d.store.ClearWorktreeAgent(agentRecord.WorktreePath)
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "agent_terminated",
		Payload: map[string]string{"id": req.ID},
	})

	// Emit stream event for visualization
	d.EmitStreamEvent(control.NewStreamEvent(control.StreamEventAgentTerminated, control.StreamSourceDaemon).
		WithAgent(req.ID).
		WithWorktree(agentRecord.WorktreePath))

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

	// Find planner agent for parent link and session ID
	var plannerAgent *store.Agent
	agents, _ := d.store.ListAgentsByWorktree(req.WorktreePath)
	for _, a := range agents {
		if a.Archetype == "planner" {
			plannerAgent = a
			break
		}
	}

	// Get plan content - try DB cache first, then Claude's storage
	var planContent string
	plan, _ := d.store.GetPlan(req.WorktreePath)
	if plan != nil && plan.Content != "" {
		planContent = plan.Content
	} else if plannerAgent != nil && plannerAgent.ClaudeSessionID != "" {
		// Read directly from Claude's plan storage
		content, err := readClaudePlan(req.WorktreePath, plannerAgent.ClaudeSessionID)
		if err != nil {
			return nil, fmt.Errorf("could not read plan: %w", err)
		}
		planContent = content
		// Cache it for future use
		if plan != nil {
			d.store.UpdatePlanContent(req.WorktreePath, content)
		}
	}

	if planContent == "" {
		return nil, fmt.Errorf("no plan content found for worktree: %s", req.WorktreePath)
	}

	// Build executor prompt with plan
	prompt := fmt.Sprintf(`## Approved Implementation Plan

%s

---

Execute this plan precisely. After each step, report what you did.`, planContent)

	// Spawn executor
	var parentID string
	if plannerAgent != nil {
		parentID = plannerAgent.ID
	}
	spec := agent.SpawnSpec{
		WorktreePath: req.WorktreePath,
		ProjectName:  wt.Project,
		Archetype:    "executor",
		Prompt:       prompt,
		ParentID:     parentID,
	}

	spawned, err := d.spawner.Spawn(d.ctx, spec)
	if err != nil {
		return nil, err
	}

	// Associate with worktree
	d.store.AssignAgentToWorktree(req.WorktreePath, spawned.ID)

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: d.agentToInfo(spawned),
	})

	return d.agentToInfo(spawned), nil
}

func (d *Daemon) agentToInfo(a *store.Agent) *control.AgentInfo {
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

	// Enrich plan status for planner agents
	if a.Archetype == "planner" {
		if plan, err := d.store.GetPlan(a.WorktreePath); err == nil && plan != nil {
			info.PlanStatus = string(plan.Status)
		}
	}

	// Compute metrics for active agents
	if a.Status != store.AgentStatusPending {
		// Try to get stored real-time metrics first
		if storedMetrics, err := d.store.GetMetricsByAgent(a.ID); err == nil && storedMetrics != nil {
			info.Metrics = &control.AgentMetrics{
				DurationMs:     storedMetrics.DurationMS,
				InputTokens:    storedMetrics.InputTokens,
				OutputTokens:   storedMetrics.OutputTokens,
				CacheReads:     storedMetrics.CacheReadTokens,
				CacheCreation:  storedMetrics.CacheCreationTokens,
				TotalTokens:    storedMetrics.TotalInputTokens(),
				CacheHitRate:   storedMetrics.CacheHitRate(),
				CostCents:      storedMetrics.CostCents,
				NumTurns:       storedMetrics.NumTurns,
				ToolUseCount:   storedMetrics.ToolCalls,
				ToolSuccessRate: float64(storedMetrics.ToolSuccesses) / float64(max(storedMetrics.ToolCalls, 1)) * 100,
			}
		}

		// Supplement with computed metrics from messages
		if computed, err := d.store.GetComputedMetrics(a.ID); err == nil && computed != nil {
			if info.Metrics == nil {
				info.Metrics = &control.AgentMetrics{
					ToolUseCount: computed.ToolUseCount,
					DurationMs:   computed.Duration.Milliseconds(),
					InputTokens:  computed.InputTokens,
					OutputTokens: computed.OutputTokens,
					CacheReads:   computed.CacheReads,
					TotalTokens:  computed.TotalTokens,
				}
			}
			// Always use computed file metrics
			info.Metrics.FilesRead = computed.FilesRead
			info.Metrics.FilesWritten = computed.FilesWritten
			info.Metrics.LinesChanged = computed.LinesChanged
			info.Metrics.MessageCount = computed.MessageCount
		}
	}

	return info
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}