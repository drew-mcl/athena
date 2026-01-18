// Package orchestrator handles multi-agent task decomposition and coordination.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/worktree"
	"github.com/google/uuid"
)

// Orchestrator coordinates multi-agent task execution.
type Orchestrator struct {
	store       *store.Store
	spawner     *agent.Spawner
	provisioner *worktree.Provisioner

	// Track active orchestrations
	tasks map[string]*TaskGraph
	mu    sync.RWMutex
}

// TaskGraph represents a decomposed task with dependencies.
type TaskGraph struct {
	ID          string
	ParentJobID string
	Subtasks    []*Subtask
	Status      string // pending | running | completed | failed

	mu sync.Mutex
}

// Subtask represents a single unit of work in a task graph.
type Subtask struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	Files        []string `json:"files,omitempty"`
	Dependencies []string `json:"dependencies"`
	Complexity   string   `json:"estimated_complexity,omitempty"`

	// Runtime state
	Status       string // pending | ready | spawning | running | completed | failed
	AgentID      string
	WorktreePath string
}

// ArchitectOutput is the expected JSON output from an architect agent.
type ArchitectOutput struct {
	Subtasks         []*Subtask `json:"subtasks"`
	MergeStrategy    string     `json:"merge_strategy"` // sequential | parallel
	IntegrationNotes string     `json:"integration_notes"`
}

// New creates a new Orchestrator.
func New(st *store.Store, sp *agent.Spawner, prov *worktree.Provisioner) *Orchestrator {
	return &Orchestrator{
		store:       st,
		spawner:     sp,
		provisioner: prov,
		tasks:       make(map[string]*TaskGraph),
	}
}

// Orchestrate decomposes a job using an architect agent, then coordinates workers.
func (o *Orchestrator) Orchestrate(ctx context.Context, job *store.Job) (*TaskGraph, error) {
	// 1. Spawn architect agent to decompose the task
	architectSpec := agent.SpawnSpec{
		WorktreePath: o.getMainWorktree(job.Project),
		ProjectName:  job.Project,
		Archetype:    "architect",
		Prompt:       fmt.Sprintf("Decompose this task into independent subtasks:\n\n%s", job.NormalizedInput),
	}

	architectAgent, err := o.spawner.Spawn(ctx, architectSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn architect: %w", err)
	}

	// 2. Wait for architect to complete and parse output
	output, err := o.waitForArchitectOutput(ctx, architectAgent.ID)
	if err != nil {
		return nil, fmt.Errorf("architect failed: %w", err)
	}

	// 3. Create task graph from output
	graph := &TaskGraph{
		ID:          uuid.NewString(),
		ParentJobID: job.ID,
		Subtasks:    output.Subtasks,
		Status:      "pending",
	}

	// Initialize subtask state
	for _, st := range graph.Subtasks {
		st.Status = "pending"
		// Check if ready (no deps or all deps met)
		if len(st.Dependencies) == 0 {
			st.Status = "ready"
		}
	}

	o.mu.Lock()
	o.tasks[graph.ID] = graph
	o.mu.Unlock()

	// 4. Start executing ready tasks
	go o.executeGraph(ctx, graph)

	return graph, nil
}

// executeGraph runs the task graph, spawning workers as dependencies are met.
func (o *Orchestrator) executeGraph(ctx context.Context, graph *TaskGraph) {
	graph.mu.Lock()
	graph.Status = "running"
	graph.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Find ready tasks
		graph.mu.Lock()
		ready := o.getReadyTasksLocked(graph)
		if len(ready) == 0 {
			// Check if all complete
			if o.allCompleteLocked(graph) {
				graph.Status = "completed"
				graph.mu.Unlock()
				return
			}
			// Check for deadlock (no ready, not all complete)
			if o.hasFailureLocked(graph) {
				graph.Status = "failed"
				graph.mu.Unlock()
				return
			}
			graph.mu.Unlock()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, task := range ready {
			task.Status = "spawning"
		}
		graph.mu.Unlock()

		// Spawn workers for ready tasks
		for _, task := range ready {
			if err := o.spawnWorker(ctx, graph, task); err != nil {
				graph.mu.Lock()
				task.Status = "failed"
				graph.mu.Unlock()
			}
		}
	}
}

// spawnWorker creates a worktree and agent for a subtask.
func (o *Orchestrator) spawnWorker(ctx context.Context, graph *TaskGraph, task *Subtask) error {
	// Get the job for project info
	job, err := o.store.GetJob(graph.ParentJobID)
	if err != nil || job == nil {
		return fmt.Errorf("job not found: %s", graph.ParentJobID)
	}

	// Create a synthetic job for the subtask to provision a worktree
	subtaskJob := &store.Job{
		ID:              task.ID,
		NormalizedInput: task.Description,
		Project:         job.Project,
	}

	// Provision worktree for this subtask
	wt, err := o.provisioner.CreateForJob(job.Project, subtaskJob)
	if err != nil {
		return fmt.Errorf("failed to provision worktree: %w", err)
	}
	graph.mu.Lock()
	task.WorktreePath = wt.Path
	graph.mu.Unlock()

	// Spawn worker agent
	workerSpec := agent.SpawnSpec{
		WorktreePath: wt.Path,
		ProjectName:  job.Project,
		Archetype:    "executor",
		Prompt:       task.Description,
		ParentID:     "", // Could link to architect agent
	}

	agent, err := o.spawner.Spawn(ctx, workerSpec)
	if err != nil {
		return fmt.Errorf("failed to spawn worker: %w", err)
	}

	graph.mu.Lock()
	task.AgentID = agent.ID
	task.Status = "running"
	graph.mu.Unlock()

	// Watch for completion
	go o.watchWorker(ctx, graph, task)

	return nil
}

// watchWorker monitors a worker agent and updates task status on completion.
func (o *Orchestrator) watchWorker(ctx context.Context, graph *TaskGraph, task *Subtask) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			agent, err := o.store.GetAgent(task.AgentID)
			if err != nil || agent == nil {
				continue
			}

			graph.mu.Lock()
			switch agent.Status {
			case store.AgentStatusCompleted:
				task.Status = "completed"
				o.updateDependentsLocked(graph, task.ID)
				graph.mu.Unlock()
				return
			case store.AgentStatusCrashed, store.AgentStatusTerminated:
				task.Status = "failed"
				graph.mu.Unlock()
				return
			default:
				graph.mu.Unlock()
			}
		}
	}
}

// updateDependents marks tasks as ready when their dependencies are met.
func (o *Orchestrator) updateDependentsLocked(graph *TaskGraph, completedID string) {
	for _, task := range graph.Subtasks {
		if task.Status != "pending" {
			continue
		}

		// Check if all dependencies are now met
		allMet := true
		for _, depID := range task.Dependencies {
			dep := o.getTask(graph, depID)
			if dep == nil || dep.Status != "completed" {
				allMet = false
				break
			}
		}

		if allMet {
			task.Status = "ready"
		}
	}
}

// Helper methods

func (o *Orchestrator) getReadyTasksLocked(graph *TaskGraph) []*Subtask {
	var ready []*Subtask
	for _, t := range graph.Subtasks {
		if t.Status == "ready" {
			ready = append(ready, t)
		}
	}
	return ready
}

func (o *Orchestrator) allCompleteLocked(graph *TaskGraph) bool {
	for _, t := range graph.Subtasks {
		if t.Status != "completed" {
			return false
		}
	}
	return true
}

func (o *Orchestrator) hasFailureLocked(graph *TaskGraph) bool {
	for _, t := range graph.Subtasks {
		if t.Status == "failed" {
			return true
		}
	}
	return false
}

func (o *Orchestrator) getTask(graph *TaskGraph, id string) *Subtask {
	for _, t := range graph.Subtasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (o *Orchestrator) getMainWorktree(project string) string {
	wts, _ := o.store.ListWorktrees(project)
	for _, wt := range wts {
		if wt.IsMain {
			return wt.Path
		}
	}
	return ""
}

func (o *Orchestrator) waitForArchitectOutput(ctx context.Context, agentID string) (*ArchitectOutput, error) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			agent, err := o.store.GetAgent(agentID)
			if err != nil || agent == nil {
				continue
			}

			if agent.Status == store.AgentStatusCompleted {
				// Get the output from the agent's events
				events, err := o.store.GetAgentEvents(agentID, 100)
				if err != nil {
					return nil, err
				}

				// Find the result event with JSON output
				for _, e := range events {
					if e.EventType == "assistant" || e.EventType == "result" || e.EventType == "text" {
						if output, ok := parseArchitectOutputPayload(e.Payload); ok {
							return output, nil
						}
					}
				}
				return nil, fmt.Errorf("no valid output from architect")
			}

			if agent.Status == store.AgentStatusCrashed || agent.Status == store.AgentStatusTerminated {
				return nil, fmt.Errorf("architect agent failed")
			}
		}
	}
}

func parseArchitectOutputPayload(payload string) (*ArchitectOutput, bool) {
	if payload == "" {
		return nil, false
	}

	var output ArchitectOutput
	if err := json.Unmarshal([]byte(payload), &output); err == nil && len(output.Subtasks) > 0 {
		return &output, true
	}

	var wrapper struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(payload), &wrapper); err != nil || wrapper.Content == "" {
		return nil, false
	}

	decoded := extractJSONBlock(wrapper.Content)
	if err := json.Unmarshal([]byte(decoded), &output); err == nil && len(output.Subtasks) > 0 {
		return &output, true
	}

	return nil, false
}

func extractJSONBlock(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	if strings.HasPrefix(trimmed, "json") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "json"))
	}

	if idx := strings.LastIndex(trimmed, "```"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}

	return trimmed
}

// GetTaskGraph returns the task graph by ID.
func (o *Orchestrator) GetTaskGraph(id string) (*TaskGraph, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	g, ok := o.tasks[id]
	return g, ok
}

// ListTaskGraphs returns all active task graphs.
func (o *Orchestrator) ListTaskGraphs() []*TaskGraph {
	o.mu.RLock()
	defer o.mu.RUnlock()

	graphs := make([]*TaskGraph, 0, len(o.tasks))
	for _, g := range o.tasks {
		graphs = append(graphs, g)
	}
	return graphs
}
