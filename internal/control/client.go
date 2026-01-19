package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

// Client connects to the athena daemon.
type Client struct {
	conn      net.Conn
	scanner   *bufio.Scanner
	mu        sync.Mutex
	pending   map[string]chan *Response
	events    chan Event
	done      chan struct{}
	connected atomic.Bool
}

// NewClient creates a new daemon client.
func NewClient(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}

	c := &Client{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
		pending: make(map[string]chan *Response),
		events:  make(chan Event, 100),
		done:    make(chan struct{}),
	}
	c.scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	c.connected.Store(true)

	go c.readLoop()
	return c, nil
}

// Close disconnects from the daemon.
func (c *Client) Close() error {
	c.connected.Store(false)
	close(c.done)
	return c.conn.Close()
}

// Events returns a channel of events from the daemon.
func (c *Client) Events() <-chan Event {
	return c.events
}

// Connected reports whether the client is still connected to the daemon.
func (c *Client) Connected() bool {
	return c.connected.Load()
}

// Call makes an RPC call to the daemon.
func (c *Client) Call(method string, params any) (*Response, error) {
	if !c.connected.Load() {
		return nil, fmt.Errorf("not connected to daemon")
	}

	id := uuid.NewString()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	req := Request{
		Method: method,
		Params: paramsJSON,
		ID:     id,
	}

	respChan := make(chan *Response, 1)
	c.mu.Lock()
	c.pending[id] = respChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	encoded, _ := json.Marshal(req)
	c.mu.Lock()
	_, err = c.conn.Write(append(encoded, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	}
}

// ListAgents retrieves all agents from the daemon.
func (c *Client) ListAgents() ([]*AgentInfo, error) {
	resp, err := c.Call("list_agents", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var agents []*AgentInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &agents)
	return agents, nil
}

// GetAgent retrieves a specific agent.
func (c *Client) GetAgent(id string) (*AgentInfo, error) {
	resp, err := c.Call("get_agent", map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var agent AgentInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &agent)
	return &agent, nil
}

// GetAgentLogs retrieves events/logs for an agent.
func (c *Client) GetAgentLogs(agentID string, limit int) ([]*AgentEventInfo, error) {
	resp, err := c.Call("get_agent_logs", map[string]any{
		"agent_id": agentID,
		"limit":    limit,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var events []*AgentEventInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &events)
	return events, nil
}

// SpawnAgent creates a new agent.
func (c *Client) SpawnAgent(req SpawnAgentRequest) (*AgentInfo, error) {
	resp, err := c.Call("spawn_agent", req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var agent AgentInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &agent)
	return &agent, nil
}

// KillAgent terminates an agent.
func (c *Client) KillAgent(id string) error {
	resp, err := c.Call("kill_agent", map[string]string{"id": id})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// ListWorktrees retrieves all worktrees.
func (c *Client) ListWorktrees() ([]*WorktreeInfo, error) {
	resp, err := c.Call("list_worktrees", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var worktrees []*WorktreeInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &worktrees)
	return worktrees, nil
}

// ListJobs retrieves all jobs.
func (c *Client) ListJobs() ([]*JobInfo, error) {
	resp, err := c.Call("list_jobs", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var jobs []*JobInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &jobs)
	return jobs, nil
}

// CreateJob creates a new job.
func (c *Client) CreateJob(req CreateJobRequest) (*JobInfo, error) {
	resp, err := c.Call("create_job", req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var job JobInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &job)
	return &job, nil
}

// NormalizePlan returns a plan for normalizing repo structure without making changes.
func (c *Client) NormalizePlan() (*NormalizePlan, error) {
	resp, err := c.Call("normalize_plan", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var plan NormalizePlan
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &plan)
	return &plan, nil
}

// Normalize reorganizes repos into Athena's standard structure.
func (c *Client) Normalize() ([]string, error) {
	resp, err := c.Call("normalize", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var result struct {
		Moved []string `json:"moved"`
	}
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &result)
	return result.Moved, nil
}

// MigratePlan returns a plan for migrating worktrees to the new structure.
func (c *Client) MigratePlan() (*MigrationPlan, error) {
	resp, err := c.Call("migrate_plan", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var plan MigrationPlan
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &plan)
	return &plan, nil
}

// MigrateWorktrees moves worktrees to the new structure.
func (c *Client) MigrateWorktrees(dryRun bool) ([]string, error) {
	resp, err := c.Call("migrate_worktrees", map[string]bool{"dry_run": dryRun})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var result struct {
		Migrated []string `json:"migrated"`
	}
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &result)
	return result.Migrated, nil
}

// CreateWorktree creates a new worktree in the dedicated worktree directory.
func (c *Client) CreateWorktree(req CreateWorktreeRequest) (*WorktreeInfo, error) {
	resp, err := c.Call("create_worktree", req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	var wt WorktreeInfo
	data, _ := json.Marshal(resp.Data)
	json.Unmarshal(data, &wt)
	return &wt, nil
}

// ListNotes retrieves all notes.
func (c *Client) ListNotes() ([]*NoteInfo, error) {
	resp, err := c.Call("list_notes", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var notes []*NoteInfo
	json.Unmarshal(data, &notes)
	return notes, nil
}

// CreateNote creates a new note.
func (c *Client) CreateNote(req CreateNoteRequest) (*NoteInfo, error) {
	resp, err := c.Call("create_note", req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var note NoteInfo
	json.Unmarshal(data, &note)
	return &note, nil
}

// UpdateNote updates a note's done status.
func (c *Client) UpdateNote(req UpdateNoteRequest) error {
	resp, err := c.Call("update_note", req)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// DeleteNote deletes a note.
func (c *Client) DeleteNote(id string) error {
	resp, err := c.Call("delete_note", map[string]string{"id": id})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// ListChangelog retrieves changelog entries.
func (c *Client) ListChangelog(project string, limit int) ([]*ChangelogInfo, error) {
	resp, err := c.Call("list_changelog", map[string]any{
		"project": project,
		"limit":   limit,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var entries []*ChangelogInfo
	json.Unmarshal(data, &entries)
	return entries, nil
}

// CreateChangelog creates a new changelog entry.
func (c *Client) CreateChangelog(req CreateChangelogRequest) (*ChangelogInfo, error) {
	resp, err := c.Call("create_changelog", req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var entry ChangelogInfo
	json.Unmarshal(data, &entry)
	return &entry, nil
}

// DeleteChangelog deletes a changelog entry.
func (c *Client) DeleteChangelog(id string) error {
	resp, err := c.Call("delete_changelog", map[string]string{"id": id})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// GetPlan retrieves the implementation plan for a worktree.
func (c *Client) GetPlan(worktreePath string, forceRefresh bool) (*PlanInfo, error) {
	resp, err := c.Call("get_plan", map[string]any{
		"worktree_path": worktreePath,
		"force_refresh": forceRefresh,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var plan PlanInfo
	json.Unmarshal(data, &plan)
	return &plan, nil
}

// ApprovePlan marks a plan as approved.
func (c *Client) ApprovePlan(worktreePath string) error {
	resp, err := c.Call("approve_plan", map[string]string{"worktree_path": worktreePath})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// SpawnExecutor spawns an executor agent with the plan as context.
func (c *Client) SpawnExecutor(worktreePath string) (*AgentInfo, error) {
	resp, err := c.Call("spawn_executor", SpawnExecutorRequest{WorktreePath: worktreePath})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var agent AgentInfo
	json.Unmarshal(data, &agent)
	return &agent, nil
}

// PublishPR pushes a worktree branch and creates a PR.
func (c *Client) PublishPR(worktreePath string) (*PublishResult, error) {
	resp, err := c.Call("publish_pr", PublishPRRequest{WorktreePath: worktreePath})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var result PublishResult
	json.Unmarshal(data, &result)
	return &result, nil
}

// MergeLocal merges a worktree branch into main locally.
func (c *Client) MergeLocal(worktreePath string) (*MergeLocalResult, error) {
	resp, err := c.Call("merge_local", map[string]string{"worktree_path": worktreePath})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	data, _ := json.Marshal(resp.Data)
	var result MergeLocalResult
	json.Unmarshal(data, &result)
	return &result, nil
}

// CleanupWorktree removes a worktree and optionally deletes the branch.
func (c *Client) CleanupWorktree(worktreePath string, deleteBranch bool) error {
	resp, err := c.Call("cleanup_worktree", CleanupWorktreeRequest{
		WorktreePath: worktreePath,
		DeleteBranch: deleteBranch,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func (c *Client) readLoop() {
	for c.scanner.Scan() {
		select {
		case <-c.done:
			return
		default:
		}

		line := c.scanner.Bytes()

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}

		if envelope.Type != "" {
			var event Event
			if json.Unmarshal(line, &event) == nil {
				select {
				case c.events <- event:
				default: // Drop if channel full
				}
			}
			continue
		}

		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		if resp.ID != "" {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- &resp
			}
			c.mu.Unlock()
		}
	}

	c.connected.Store(false)
}

// AgentInfo represents agent data for API responses.
type AgentInfo struct {
	ID              string `json:"id"`
	WorktreePath    string `json:"worktree_path"`
	ProjectName     string `json:"project_name"`
	Project         string `json:"project"` // Alias for ProjectName (for filtering)
	Archetype       string `json:"archetype"`
	Status          string `json:"status"`
	Prompt          string `json:"prompt,omitempty"`
	RestartCount    int    `json:"restart_count"`
	CreatedAt       string `json:"created_at"`
	LinearIssueID   string `json:"linear_issue_id,omitempty"`
	ClaudeSessionID string `json:"claude_session_id,omitempty"` // For claude --resume
	// Activity tracking - what the agent is currently doing
	LastActivity     string `json:"last_activity,omitempty"`      // Human-readable current action
	LastActivityTime string `json:"last_activity_time,omitempty"` // When the activity happened
	LastEventType    string `json:"last_event_type,omitempty"`    // Raw event type
}

// AgentEventInfo represents an agent event for API responses.
type AgentEventInfo struct {
	ID        int64  `json:"id"`
	AgentID   string `json:"agent_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	Timestamp string `json:"timestamp"`
}

// WorktreeInfo represents worktree data for API responses.
type WorktreeInfo struct {
	Path    string `json:"path"`
	Project string `json:"project"`
	Branch  string `json:"branch"`
	IsMain  bool   `json:"is_main"`
	AgentID string `json:"agent_id,omitempty"`
	Status  string `json:"status"` // Git status (dirty/clean indicators)
	// New fields for ticket-based workflow
	TicketID    string `json:"ticket_id,omitempty"`    // External ticket ID (e.g., ENG-123)
	TicketHash  string `json:"ticket_hash,omitempty"`  // 4-char hash for uniqueness
	Description string `json:"description,omitempty"`  // Worktree description/purpose
	ProjectName string `json:"project_name,omitempty"` // Cached from git remote origin
	WTStatus    string `json:"wt_status,omitempty"`    // Worktree lifecycle: active | published | merged | stale
	PRURL       string `json:"pr_url,omitempty"`       // GitHub PR URL if published
}

// JobInfo represents job data for API responses.
type JobInfo struct {
	ID              string `json:"id"`
	RawInput        string `json:"raw_input"`
	NormalizedInput string `json:"normalized_input"`
	Status          string `json:"status"`
	Type            string `json:"type"` // question | quick | feature
	Project         string `json:"project"`
	CreatedAt       string `json:"created_at"`
	AgentID         string `json:"agent_id,omitempty"`
	ExternalID      string `json:"external_id,omitempty"`   // Linear/Jira ticket ID
	ExternalURL     string `json:"external_url,omitempty"`  // Link to external tracker
	Answer          string `json:"answer,omitempty"`        // Response for question jobs
	WorktreePath    string `json:"worktree_path,omitempty"` // For quick jobs
}

// SpawnAgentRequest is the request to spawn a new agent.
type SpawnAgentRequest struct {
	WorktreePath string `json:"worktree_path"`
	Archetype    string `json:"archetype"`
	Prompt       string `json:"prompt"`
}

// CreateJobRequest is the request to create a new job.
type CreateJobRequest struct {
	Input        string `json:"input"`
	Project      string `json:"project"`
	Type         string `json:"type,omitempty"`          // question | quick | feature (default: feature)
	ExternalID   string `json:"external_id,omitempty"`   // Optional Linear/Jira ID
	TargetBranch string `json:"target_branch,omitempty"` // For quick jobs (default: main)
}

// NormalizePlan describes what normalize would do.
type NormalizePlan struct {
	BaseDir string          `json:"base_dir"`
	Moves   []NormalizeMove `json:"moves"`
}

// NormalizeMove describes a single repo movement.
type NormalizeMove struct {
	Project     string         `json:"project"`
	CurrentPath string         `json:"current_path"`
	TargetPath  string         `json:"target_path"`
	IsMain      bool           `json:"is_main"`
	Worktrees   []WorktreeMove `json:"worktrees,omitempty"`
}

// WorktreeMove describes a worktree movement.
type WorktreeMove struct {
	CurrentPath string `json:"current_path"`
	TargetPath  string `json:"target_path"`
	Branch      string `json:"branch"`
}

// NoteInfo represents note data for API responses.
type NoteInfo struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Done      bool   `json:"done"`
	CreatedAt string `json:"created_at"`
}

// CreateNoteRequest is the request to create a new note.
type CreateNoteRequest struct {
	Content string `json:"content"`
}

// UpdateNoteRequest is the request to update a note.
type UpdateNoteRequest struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

// ChangelogInfo represents changelog data for API responses.
type ChangelogInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"` // feature | fix | refactor | docs
	Project     string `json:"project"`
	JobID       string `json:"job_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// CreateChangelogRequest is the request to create a changelog entry.
type CreateChangelogRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"` // feature | fix | refactor | docs
	Project     string `json:"project"`
	JobID       string `json:"job_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
}

// CreateWorktreeRequest is the request to create a new worktree.
type CreateWorktreeRequest struct {
	MainRepoPath string `json:"main_repo_path"` // Path to the main repository
	Branch       string `json:"branch"`         // Branch name (optional, will be generated)
	TicketID     string `json:"ticket_id"`      // Ticket ID (e.g., ENG-123)
	Description  string `json:"description"`    // Description of the work
}

// MigrationPlan describes what migration would do.
type MigrationPlan struct {
	WorktreeDir string          `json:"worktree_dir"`
	Migrations  []MigrationItem `json:"migrations"`
}

// MigrationItem describes a single worktree migration.
type MigrationItem struct {
	CurrentPath string `json:"current_path"`
	TargetPath  string `json:"target_path"`
	Branch      string `json:"branch"`
	TicketID    string `json:"ticket_id"`
	Hash        string `json:"hash"`
	Project     string `json:"project"`
}

// PlanInfo represents plan data for API responses.
type PlanInfo struct {
	ID            string `json:"id"`
	WorktreePath  string `json:"worktree_path"`
	AgentID       string `json:"agent_id"`
	Content       string `json:"content"`
	Status        string `json:"status"`         // pending | draft | approved | executing | completed
	PlannerStatus string `json:"planner_status"` // Status of the planner agent (for visibility when pending)
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// SpawnExecutorRequest is the request to spawn an executor agent.
type SpawnExecutorRequest struct {
	WorktreePath string `json:"worktree_path"`
}

// PublishPRRequest is the request to publish a worktree via PR.
type PublishPRRequest struct {
	WorktreePath string `json:"worktree_path"`
	Title        string `json:"title,omitempty"` // Optional: auto-generated if empty
	Body         string `json:"body,omitempty"`  // Optional: auto-generated if empty
}

// PublishResult contains the result of publishing a PR.
type PublishResult struct {
	PRURL  string `json:"pr_url"`
	Branch string `json:"branch"`
}

// MergeLocalResult contains the result of a local merge attempt.
type MergeLocalResult struct {
	Success      bool   `json:"success"`
	HasConflicts bool   `json:"has_conflicts,omitempty"`
	AgentSpawned bool   `json:"agent_spawned,omitempty"` // True if resolver agent was spawned
	AgentID      string `json:"agent_id,omitempty"`      // ID of spawned resolver agent
	Message      string `json:"message,omitempty"`
}

// CleanupWorktreeRequest is the request to cleanup a worktree.
type CleanupWorktreeRequest struct {
	WorktreePath string `json:"worktree_path"`
	DeleteBranch bool   `json:"delete_branch"` // Whether to also delete the branch
}
