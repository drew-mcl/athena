package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

// autoRunState tracks the state of a running auto-run loop.
type autoRunState struct {
	mu             sync.Mutex
	running        bool
	project        string
	cancel         context.CancelFunc
	currentItem    *store.WorkItem
	currentAgent   string
	completedItems []control.AutoRunResult
	failedItems    []control.AutoRunResult
	startedAt      time.Time
}

func (d *Daemon) registerAutoRunHandlers() {
	d.server.Handle("start_auto_run", d.handleStartAutoRun)
	d.server.Handle("stop_auto_run", d.handleStopAutoRun)
	d.server.Handle("auto_run_status", d.handleAutoRunStatus)
}

func (d *Daemon) handleStartAutoRun(params json.RawMessage) (any, error) {
	var req control.AutoRunRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	project := req.Project
	if project == "" {
		project = "default"
	}

	if d.autoRun != nil {
		d.autoRun.mu.Lock()
		running := d.autoRun.running
		d.autoRun.mu.Unlock()
		if running {
			return nil, fmt.Errorf("auto-run already active for project %q", d.autoRun.project)
		}
	}

	ctx, cancel := context.WithCancel(d.ctx)
	state := &autoRunState{
		running:   true,
		project:   project,
		cancel:    cancel,
		startedAt: time.Now(),
	}
	d.autoRun = state

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.autoRunLoop(ctx, state, req.Once)
	}()

	// Return initial status
	return d.buildAutoRunStatus(state), nil
}

func (d *Daemon) handleStopAutoRun(_ json.RawMessage) (any, error) {
	if d.autoRun == nil {
		return nil, fmt.Errorf("no auto-run session active")
	}

	d.autoRun.mu.Lock()
	defer d.autoRun.mu.Unlock()

	if !d.autoRun.running {
		return nil, fmt.Errorf("auto-run is not running")
	}

	d.autoRun.cancel()
	d.autoRun.running = false

	logging.Info("auto-run stopped by user", "project", d.autoRun.project)
	return d.buildAutoRunStatus(d.autoRun), nil
}

func (d *Daemon) handleAutoRunStatus(_ json.RawMessage) (any, error) {
	if d.autoRun == nil {
		return &control.AutoRunStatus{Running: false}, nil
	}
	d.autoRun.mu.Lock()
	defer d.autoRun.mu.Unlock()
	return d.buildAutoRunStatus(d.autoRun), nil
}

func (d *Daemon) buildAutoRunStatus(state *autoRunState) *control.AutoRunStatus {
	status := &control.AutoRunStatus{
		Running:        state.running,
		Project:        state.project,
		Completed:      len(state.completedItems),
		Failed:         len(state.failedItems),
		CompletedItems: state.completedItems,
		FailedItems:    state.failedItems,
	}
	if state.currentItem != nil {
		status.CurrentItem = workItemToInfo(state.currentItem)
	}
	if state.currentAgent != "" {
		agent, err := d.store.GetAgent(state.currentAgent)
		if err == nil && agent != nil {
			status.CurrentAgent = d.agentToInfo2(agent)
		}
	}
	return status
}

// autoRunLoop is the main loop that picks tasks and spawns agents.
func (d *Daemon) autoRunLoop(ctx context.Context, state *autoRunState, once bool) {
	defer func() {
		state.mu.Lock()
		state.running = false
		state.mu.Unlock()

		logging.Info("auto-run loop finished",
			"project", state.project,
			"completed", len(state.completedItems),
			"failed", len(state.failedItems),
		)

		d.server.Broadcast(control.Event{
			Type:    "auto_run_finished",
			Payload: d.buildAutoRunStatus(state),
		})
	}()

	logging.Info("auto-run loop started", "project", state.project, "once", once)

	d.server.Broadcast(control.Event{
		Type:    "auto_run_started",
		Payload: d.buildAutoRunStatus(state),
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check rate limit before spawning next item
		if limited, resetAt := d.rateLimit.isRateLimited(); limited {
			logging.Info("auto-run: rate limited, waiting", "reset_at", resetAt)
			d.server.Broadcast(control.Event{
				Type:    "auto_run_rate_limited",
				Payload: map[string]any{"reset_at": resetAt.Format(time.RFC3339)},
			})
			if err := d.rateLimit.waitForRateLimit(ctx); err != nil {
				return // context cancelled
			}
			logging.Info("auto-run: rate limit cleared, resuming")

			// Recover rate-limited agents
			d.rateLimitRecovery()
		}

		// 1. Pick next ready item (prefer features, then tasks)
		item, err := d.pickNextItem(state.project)
		if err != nil {
			logging.Error("auto-run: failed to pick next item", "error", err)
			return
		}
		if item == nil {
			logging.Info("auto-run: no more ready items", "project", state.project)
			return
		}

		state.mu.Lock()
		state.currentItem = item
		state.mu.Unlock()

		logging.Info("auto-run: picked item", "id", item.ID, "subject", item.Subject, "type", item.ItemType)

		// 2. Spawn agent on this item
		agentID, err := d.autoRunSpawn(ctx, state, item)
		if err != nil {
			logging.Error("auto-run: failed to spawn agent", "item", item.ID, "error", err)
			state.mu.Lock()
			state.failedItems = append(state.failedItems, control.AutoRunResult{
				ItemID:   item.ID,
				Subject:  item.Subject,
				ItemType: string(item.ItemType),
				Error:    fmt.Sprintf("spawn failed: %v", err),
			})
			state.currentItem = nil
			state.mu.Unlock()

			if once {
				return
			}
			continue
		}

		state.mu.Lock()
		state.currentAgent = agentID
		state.mu.Unlock()

		// 3. Wait for agent to complete
		exitCode, err := d.waitForAgent(ctx, agentID)
		if err != nil {
			logging.Warn("auto-run: agent wait interrupted", "agent", agentID, "error", err)
			state.mu.Lock()
			state.currentItem = nil
			state.currentAgent = ""
			state.mu.Unlock()
			return
		}

		// 4. Check result
		if exitCode == 0 {
			// For features, verify a PR was actually opened
			if item.ItemType == store.WorkItemTypeFeature && item.WorktreePath != nil {
				if !hasPullRequest(*item.WorktreePath) {
					logging.Warn("auto-run: agent exited 0 but no PR found", "item", item.ID, "agent", agentID)
					state.mu.Lock()
					state.failedItems = append(state.failedItems, control.AutoRunResult{
						ItemID:   item.ID,
						Subject:  item.Subject,
						ItemType: string(item.ItemType),
						AgentID:  agentID,
						ExitCode: exitCode,
						Error:    "agent completed but no PR was opened",
					})
					state.mu.Unlock()
					goto next
				}
			}

			logging.Info("auto-run: agent completed successfully", "item", item.ID, "agent", agentID)
			state.mu.Lock()
			state.completedItems = append(state.completedItems, control.AutoRunResult{
				ItemID:   item.ID,
				Subject:  item.Subject,
				ItemType: string(item.ItemType),
				AgentID:  agentID,
				ExitCode: exitCode,
			})
			state.mu.Unlock()

			// Mark work item as completed
			item.Status = store.WorkItemStatusCompleted
			if err := d.store.UpdateWorkItem(item); err != nil {
				logging.Warn("auto-run: failed to mark item completed", "item", item.ID, "error", err)
			}
		} else {
			logging.Warn("auto-run: agent failed", "item", item.ID, "agent", agentID, "exit_code", exitCode)
			state.mu.Lock()
			state.failedItems = append(state.failedItems, control.AutoRunResult{
				ItemID:   item.ID,
				Subject:  item.Subject,
				ItemType: string(item.ItemType),
				AgentID:  agentID,
				ExitCode: exitCode,
				Error:    fmt.Sprintf("agent exited with code %d", exitCode),
			})
			state.mu.Unlock()
		}
	next:

		state.mu.Lock()
		state.currentItem = nil
		state.currentAgent = ""
		state.mu.Unlock()

		d.server.Broadcast(control.Event{
			Type:    "auto_run_progress",
			Payload: d.buildAutoRunStatus(state),
		})

		if once {
			return
		}

		// Brief pause between tasks to avoid hammering
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// pickNextItem selects the best ready item to work on.
// Priority: features first (they produce PRs), then tasks.
func (d *Daemon) pickNextItem(project string) (*store.WorkItem, error) {
	items, err := d.store.ListReadyItems(project)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, nil
	}

	// Prefer features over tasks (features produce PRs and move the needle)
	for _, item := range items {
		if item.ItemType == store.WorkItemTypeFeature {
			return item, nil
		}
	}

	// Fall back to first available item
	return items[0], nil
}

// autoRunSpawn spawns a headless agent on a work item using the standard spawn flow.
func (d *Daemon) autoRunSpawn(ctx context.Context, state *autoRunState, item *store.WorkItem) (string, error) {
	// Build a spawn request for this item
	req := control.SpawnRequest{
		Project:  item.Project,
		Headless: true,
	}

	// Route based on item type
	switch item.ItemType {
	case store.WorkItemTypeFeature:
		req.FeatureID = item.ID
	default:
		req.WorkItemID = item.ID
		// For non-feature items, we need a work dir
		mainRepo, err := d.findMainRepoPath(item.Project)
		if err != nil {
			return "", fmt.Errorf("cannot find repo for project: %w", err)
		}
		req.WorkDir = mainRepo
	}

	resp, err := d.handleSpawn(json.RawMessage(mustJSON(req)))
	if err != nil {
		return "", err
	}

	spawnResp, ok := resp.(*control.SpawnResponse)
	if !ok {
		return "", fmt.Errorf("unexpected spawn response type")
	}

	if spawnResp.Agent == nil {
		return "", fmt.Errorf("spawn did not return agent info")
	}

	return spawnResp.Agent.ID, nil
}

// waitForAgent polls until the agent reaches a terminal state.
func (d *Daemon) waitForAgent(ctx context.Context, agentID string) (int, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-ticker.C:
			agent, err := d.store.GetAgent(agentID)
			if err != nil {
				return -1, fmt.Errorf("failed to get agent: %w", err)
			}
			if agent == nil {
				return -1, fmt.Errorf("agent not found: %s", agentID)
			}

			switch agent.Status {
			case store.AgentStatusCompleted:
				exitCode := 0
				if agent.ExitCode != nil {
					exitCode = *agent.ExitCode
				}
				return exitCode, nil
			case store.AgentStatusCrashed:
				exitCode := 1
				if agent.ExitCode != nil {
					exitCode = *agent.ExitCode
				}
				return exitCode, nil
			case store.AgentStatusTerminated:
				return -1, fmt.Errorf("agent was terminated")
			case store.AgentStatusRateLimited:
				// Agent is paused due to rate limit - wait for it to be recovered
				// The auto-run loop or recovery goroutine will reset it to crashed
				// and the supervisor will restart it
			}
			// Still running, keep polling
		}
	}
}

// mustJSON marshals a value to json.RawMessage, panicking on error.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}

// agentToInfo2 converts a store.Agent to control.AgentInfo.
// This is a simpler version that works with a store.Agent directly.
// hasPullRequest checks if a PR exists for the current branch in the worktree.
func hasPullRequest(worktreePath string) bool {
	cmd := exec.Command("gh", "pr", "view", "--json", "number", "--jq", ".number")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func (d *Daemon) agentToInfo2(agent *store.Agent) *control.AgentInfo {
	return &control.AgentInfo{
		ID:              agent.ID,
		Status:          string(agent.Status),
		WorktreePath:    agent.WorktreePath,
		Archetype:       agent.Archetype,
		ClaudeSessionID: agent.ClaudeSessionID,
	}
}
