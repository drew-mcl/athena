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
	conn         net.Conn
	scanner      *bufio.Scanner
	mu           sync.Mutex
	pending      map[string]chan *Response
	events       chan Event
	streamEvents chan *StreamEvent // Stream events for visualization
	done         chan struct{}
	connected    atomic.Bool
}

// NewClient creates a new daemon client.
func NewClient(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}

	c := &Client{
		conn:         conn,
		scanner:      bufio.NewScanner(conn),
		pending:      make(map[string]chan *Response),
		events:       make(chan Event, 100),
		streamEvents: make(chan *StreamEvent, 1000), // Larger buffer for stream events
		done:         make(chan struct{}),
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

// StreamEvents returns a channel of stream events for visualization.
// Call SubscribeStream first to enable stream mode.
func (c *Client) StreamEvents() <-chan *StreamEvent {
	return c.streamEvents
}

// SubscribeStream enables stream mode to receive real-time events.
// Filter options allow narrowing which events are received.
func (c *Client) SubscribeStream(req SubscribeStreamRequest) (*SubscribeStreamResponse, error) {
	var result SubscribeStreamResponse
	if err := c.callAndDecode("subscribe_stream", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SubscribeStreamResponse contains the subscription confirmation.
type SubscribeStreamResponse struct {
	Subscribed        bool                   `json:"subscribed"`
	Filter            SubscribeStreamRequest `json:"filter"`
	ActiveAgents      int                    `json:"active_agents"`
	StreamSubscribers int                    `json:"stream_subscribers"`
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

	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
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

func responseError(resp *Response) error {
	if resp != nil && resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func decodeResponseData(data any, out any) error {
	if out == nil {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal response data: %w", err)
	}
	if err := json.Unmarshal(encoded, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

func (c *Client) callAndDecode(method string, params any, out any) error {
	resp, err := c.Call(method, params)
	if err != nil {
		return err
	}
	if err := responseError(resp); err != nil {
		return err
	}
	return decodeResponseData(resp.Data, out)
}

// ListAgents retrieves all agents from the daemon.
func (c *Client) ListAgents() ([]*AgentInfo, error) {
	var agents []*AgentInfo
	if err := c.callAndDecode("list_agents", nil, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// GetAgent retrieves a specific agent.
func (c *Client) GetAgent(id string) (*AgentInfo, error) {
	var agent AgentInfo
	if err := c.callAndDecode("get_agent", map[string]string{"id": id}, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// GetAgentLogs retrieves events/logs for an agent.
func (c *Client) GetAgentLogs(agentID string, limit int) ([]*AgentEventInfo, error) {
	var events []*AgentEventInfo
	if err := c.callAndDecode("get_agent_logs", map[string]any{
		"agent_id": agentID,
		"limit":    limit,
	}, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// SpawnAgent creates a new agent.
func (c *Client) SpawnAgent(req SpawnAgentRequest) (*AgentInfo, error) {
	var agent AgentInfo
	if err := c.callAndDecode("spawn_agent", req, &agent); err != nil {
		return nil, err
	}
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

// KillAgentWithDelete terminates an agent and optionally deletes all associated data.
// When delete is true, it performs a cascade delete removing all dependent records.
func (c *Client) KillAgentWithDelete(id string, deleteData bool) error {
	resp, err := c.Call("kill_agent", map[string]any{"id": id, "delete": deleteData})
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
	return c.ListWorktreesWithOptions(ListWorktreesRequest{})
}

// ListWorktreesWithOptions retrieves worktrees with optional fields.
func (c *Client) ListWorktreesWithOptions(req ListWorktreesRequest) ([]*WorktreeInfo, error) {
	var worktrees []*WorktreeInfo
	if err := c.callAndDecode("list_worktrees", req, &worktrees); err != nil {
		return nil, err
	}
	return worktrees, nil
}

// ListJobs retrieves all jobs.
func (c *Client) ListJobs() ([]*JobInfo, error) {
	var jobs []*JobInfo
	if err := c.callAndDecode("list_jobs", nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// CreateJob creates a new job.
func (c *Client) CreateJob(req CreateJobRequest) (*JobInfo, error) {
	var job JobInfo
	if err := c.callAndDecode("create_job", req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// NormalizePlan returns a plan for normalizing repo structure without making changes.
func (c *Client) NormalizePlan() (*NormalizePlan, error) {
	var plan NormalizePlan
	if err := c.callAndDecode("normalize_plan", nil, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// Normalize reorganizes repos into Athena's standard structure.
func (c *Client) Normalize() ([]string, error) {
	var result struct {
		Moved []string `json:"moved"`
	}
	if err := c.callAndDecode("normalize", nil, &result); err != nil {
		return nil, err
	}
	return result.Moved, nil
}

// MigratePlan returns a plan for migrating worktrees to the new structure.
func (c *Client) MigratePlan() (*MigrationPlan, error) {
	var plan MigrationPlan
	if err := c.callAndDecode("migrate_plan", nil, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// MigrateWorktrees moves worktrees to the new structure.
func (c *Client) MigrateWorktrees(dryRun bool) ([]string, error) {
	var result struct {
		Migrated []string `json:"migrated"`
	}
	if err := c.callAndDecode("migrate_worktrees", map[string]bool{"dry_run": dryRun}, &result); err != nil {
		return nil, err
	}
	return result.Migrated, nil
}

// CreateWorktree creates a new worktree in the dedicated worktree directory.
func (c *Client) CreateWorktree(req CreateWorktreeRequest) (*WorktreeInfo, error) {
	var wt WorktreeInfo
	if err := c.callAndDecode("create_worktree", req, &wt); err != nil {
		return nil, err
	}
	return &wt, nil
}

// ListNotes retrieves all notes.
func (c *Client) ListNotes() ([]*NoteInfo, error) {
	var notes []*NoteInfo
	if err := c.callAndDecode("list_notes", nil, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

// CreateNote creates a new note.
func (c *Client) CreateNote(req CreateNoteRequest) (*NoteInfo, error) {
	var note NoteInfo
	if err := c.callAndDecode("create_note", req, &note); err != nil {
		return nil, err
	}
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
	var entries []*ChangelogInfo
	if err := c.callAndDecode("list_changelog", map[string]any{
		"project": project,
		"limit":   limit,
	}, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// CreateChangelog creates a new changelog entry.
func (c *Client) CreateChangelog(req CreateChangelogRequest) (*ChangelogInfo, error) {
	var entry ChangelogInfo
	if err := c.callAndDecode("create_changelog", req, &entry); err != nil {
		return nil, err
	}
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
	var plan PlanInfo
	if err := c.callAndDecode("get_plan", map[string]any{
		"worktree_path": worktreePath,
		"force_refresh": forceRefresh,
	}, &plan); err != nil {
		return nil, err
	}
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
	var agent AgentInfo
	if err := c.callAndDecode("spawn_executor", SpawnExecutorRequest{WorktreePath: worktreePath}, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// PublishPR pushes a worktree branch and creates a PR.
func (c *Client) PublishPR(worktreePath string) (*PublishResult, error) {
	var result PublishResult
	if err := c.callAndDecode("publish_pr", PublishPRRequest{WorktreePath: worktreePath}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MergeLocal merges a worktree branch into main locally.
func (c *Client) MergeLocal(worktreePath string) (*MergeLocalResult, error) {
	var result MergeLocalResult
	if err := c.callAndDecode("merge_local", map[string]string{"worktree_path": worktreePath}, &result); err != nil {
		return nil, err
	}
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

// AbandonWorktree forcibly removes a worktree (even with uncommitted changes),
// deletes the branch, and restores the source note if one exists.
func (c *Client) AbandonWorktree(worktreePath string) error {
	resp, err := c.Call("abandon_worktree", AbandonWorktreeRequest{
		WorktreePath: worktreePath,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// GetBlackboard retrieves all blackboard entries for a worktree.
func (c *Client) GetBlackboard(worktreePath string) ([]*BlackboardEntryInfo, error) {
	var entries []*BlackboardEntryInfo
	if err := c.callAndDecode("get_blackboard", map[string]string{"worktree_path": worktreePath}, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// PostBlackboard posts a new entry to the blackboard.
func (c *Client) PostBlackboard(req PostBlackboardRequest) (*BlackboardEntryInfo, error) {
	var entry BlackboardEntryInfo
	if err := c.callAndDecode("post_blackboard", req, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// ClearBlackboard removes all entries for a worktree.
func (c *Client) ClearBlackboard(worktreePath string) error {
	resp, err := c.Call("clear_blackboard", map[string]string{"worktree_path": worktreePath})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// GetBlackboardSummary retrieves statistics for a worktree's blackboard.
func (c *Client) GetBlackboardSummary(worktreePath string) (*BlackboardSummaryInfo, error) {
	var summary BlackboardSummaryInfo
	if err := c.callAndDecode("get_blackboard_summary", map[string]string{"worktree_path": worktreePath}, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// GetProjectState retrieves all state entries for a project.
func (c *Client) GetProjectState(project string) ([]*StateEntryInfo, error) {
	var entries []*StateEntryInfo
	if err := c.callAndDecode("get_project_state", map[string]string{"project": project}, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SetProjectState creates or updates a project state entry.
func (c *Client) SetProjectState(req SetStateRequest) (*StateEntryInfo, error) {
	var entry StateEntryInfo
	if err := c.callAndDecode("set_project_state", req, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetStateSummary retrieves statistics for a project's state.
func (c *Client) GetStateSummary(project string) (*StateSummaryInfo, error) {
	var summary StateSummaryInfo
	if err := c.callAndDecode("get_state_summary", map[string]string{"project": project}, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// GetContextPreview retrieves a formatted preview of context an agent would see.
func (c *Client) GetContextPreview(worktreePath, projectName string) (string, error) {
	var result struct {
		Context string `json:"context"`
	}
	if err := c.callAndDecode("get_context_preview", map[string]string{
		"worktree_path": worktreePath,
		"project_name":  projectName,
	}, &result); err != nil {
		return "", err
	}
	return result.Context, nil
}

// GetCacheStats retrieves cross-agent cache hit rate statistics for a project.
func (c *Client) GetCacheStats(projectName string) (*ProjectCacheStatsInfo, error) {
	var stats ProjectCacheStatsInfo
	if err := c.callAndDecode("get_cache_stats", map[string]string{"project_name": projectName}, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (c *Client) readLoop() {
	for c.scanner.Scan() {
		select {
		case <-c.done:
			return
		default:
		}

		line := c.scanner.Bytes()

		// Check for stream event envelope first
		var streamEnvelope struct {
			Stream bool         `json:"stream"`
			Event  *StreamEvent `json:"event"`
		}
		if err := json.Unmarshal(line, &streamEnvelope); err == nil && streamEnvelope.Stream {
			if streamEnvelope.Event != nil {
				select {
				case c.streamEvents <- streamEnvelope.Event:
				default: // Drop if channel full
				}
			}
			continue
		}

		// Check for regular broadcast event
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

		// Handle RPC response
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
	// Plan info (enriched by daemon)
	PlanStatus string `json:"plan_status,omitempty"` // pending | draft | approved | executing | completed
	PlanPath   string `json:"plan_path,omitempty"`   // path to plan file if exists
	// Task list
	TaskListID string `json:"task_list_id,omitempty"` // work item ID used as task list
	// Usage metrics
	Metrics *AgentMetrics `json:"metrics,omitempty"`
}

// AgentMetrics holds usage statistics for an agent.
type AgentMetrics struct {
	// Tool usage
	ToolUseCount    int     `json:"tool_use_count"`
	ToolSuccessRate float64 `json:"tool_success_rate,omitempty"` // Percentage

	// File activity
	FilesRead    int `json:"files_read"`
	FilesWritten int `json:"files_written"`
	LinesChanged int `json:"lines_changed"`
	MessageCount int `json:"message_count"`

	// Timing
	DurationMs int64 `json:"duration_ms"` // Wall time
	APITimeMs  int64 `json:"api_time_ms,omitempty"`
	NumTurns   int   `json:"num_turns,omitempty"`

	// Token usage
	InputTokens   int `json:"input_tokens,omitempty"`
	OutputTokens  int `json:"output_tokens,omitempty"`
	CacheReads    int `json:"cache_reads,omitempty"`
	CacheCreation int `json:"cache_creation,omitempty"`
	TotalTokens   int `json:"total_tokens,omitempty"`

	// Cache efficiency
	CacheHitRate float64 `json:"cache_hit_rate,omitempty"` // Percentage

	// Cost
	CostCents int `json:"cost_cents,omitempty"`
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
	TicketID     string `json:"ticket_id,omitempty"`      // External ticket ID (e.g., ENG-123)
	TicketHash   string `json:"ticket_hash,omitempty"`    // 4-char hash for uniqueness
	Description  string `json:"description,omitempty"`    // Worktree description/purpose
	ProjectName  string `json:"project_name,omitempty"`   // Cached from git remote origin
	WTStatus     string `json:"wt_status,omitempty"`      // Worktree lifecycle: active | published | merged | stale
	PRURL        string `json:"pr_url,omitempty"`         // GitHub PR URL if published
	Summary      string `json:"summary,omitempty"`        // Plan summary from frontmatter
	SourceNoteID string `json:"source_note_id,omitempty"` // Note ID if promoted from note
	Ahead        int    `json:"ahead,omitempty"`          // Commits ahead of upstream
	Behind       int    `json:"behind,omitempty"`         // Commits behind upstream
}

// ListWorktreesRequest controls optional fields for worktree listing.
type ListWorktreesRequest struct {
	IncludeStatus  *bool `json:"include_status,omitempty"`
	IncludeSummary *bool `json:"include_summary,omitempty"`
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
	Provider     string `json:"provider"`
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
	WorkflowMode string `json:"workflow_mode"`  // Workflow mode: automatic, approve, or manual
	Provider     string `json:"provider"`       // AI Provider (claude, gemini, etc.)
	SourceNoteID string `json:"source_note_id"` // Note ID if promoted from note (for abandon rollback)
	StartPoint   string `json:"start_point"`    // Git ref to start from (optional, auto-uses queue head)
	UseQueueHead bool   `json:"use_queue_head"` // If true, auto-start from merge queue head (default: true)
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
	Summary       string `json:"summary,omitempty"` // Brief summary extracted from frontmatter
	Status        string `json:"status"`            // pending | draft | approved | executing | completed
	PlannerStatus string `json:"planner_status"`    // Status of the planner agent (for visibility when pending)
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

// AbandonWorktreeRequest is the request to abandon a worktree (force delete, restore note).
type AbandonWorktreeRequest struct {
	WorktreePath string `json:"worktree_path"`
}

// BlackboardEntryInfo represents a blackboard entry for API responses.
type BlackboardEntryInfo struct {
	ID           string `json:"id"`
	WorktreePath string `json:"worktree_path"`
	EntryType    string `json:"entry_type"` // decision | finding | attempt | question | artifact
	Content      string `json:"content"`
	AgentID      string `json:"agent_id"`
	Sequence     int    `json:"sequence"`
	CreatedAt    string `json:"created_at"`
	Resolved     bool   `json:"resolved"`
	ResolvedBy   string `json:"resolved_by,omitempty"`
}

// StateEntryInfo represents a project state entry for API responses.
type StateEntryInfo struct {
	ID          string  `json:"id"`
	Project     string  `json:"project"`
	StateType   string  `json:"state_type"` // architecture | convention | constraint | decision | environment
	Key         string  `json:"key"`
	Value       string  `json:"value"`
	Confidence  float64 `json:"confidence"`
	SourceAgent string  `json:"source_agent,omitempty"`
	SourceRef   string  `json:"source_ref,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// BlackboardSummaryInfo provides statistics about blackboard entries.
type BlackboardSummaryInfo struct {
	WorktreePath    string `json:"worktree_path"`
	DecisionCount   int    `json:"decision_count"`
	FindingCount    int    `json:"finding_count"`
	AttemptCount    int    `json:"attempt_count"`
	QuestionCount   int    `json:"question_count"`
	UnresolvedCount int    `json:"unresolved_count"`
	ArtifactCount   int    `json:"artifact_count"`
	TotalCount      int    `json:"total_count"`
}

// StateSummaryInfo provides statistics about project state entries.
type StateSummaryInfo struct {
	Project           string  `json:"project"`
	ArchitectureCount int     `json:"architecture_count"`
	ConventionCount   int     `json:"convention_count"`
	ConstraintCount   int     `json:"constraint_count"`
	DecisionCount     int     `json:"decision_count"`
	EnvironmentCount  int     `json:"environment_count"`
	TotalCount        int     `json:"total_count"`
	AvgConfidence     float64 `json:"avg_confidence"`
}

// ProjectCacheStatsInfo provides aggregated cache hit rate statistics for a project.
type ProjectCacheStatsInfo struct {
	ProjectName                 string  `json:"project_name"`
	TotalAgents                 int     `json:"total_agents"`
	FirstAgentCount             int     `json:"first_agent_count"`
	SubsequentAgentCount        int     `json:"subsequent_agent_count"`
	AvgCacheHitRate             float64 `json:"avg_cache_hit_rate"`
	AvgFirstAgentCacheRate      float64 `json:"avg_first_agent_cache_rate"`
	AvgSubsequentAgentCacheRate float64 `json:"avg_subsequent_agent_cache_rate"`
	TotalStateTokens            int     `json:"total_state_tokens"`
	TotalBlackboardTokens       int     `json:"total_blackboard_tokens"`
	TotalCacheReads             int     `json:"total_cache_reads"`
}

// PostBlackboardRequest is the request to post a blackboard entry.
type PostBlackboardRequest struct {
	WorktreePath string `json:"worktree_path"`
	EntryType    string `json:"entry_type"` // decision | finding | attempt | question | artifact
	Content      string `json:"content"`
	AgentID      string `json:"agent_id,omitempty"` // Optional, defaults to "manual"
}

// SetStateRequest is the request to set a project state entry.
type SetStateRequest struct {
	Project    string  `json:"project"`
	StateType  string  `json:"state_type"` // architecture | convention | constraint | decision | environment
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"` // Defaults to 1.0
	AgentID    string  `json:"agent_id,omitempty"`   // Optional source agent
}

// TaskListInfo represents a task list for API responses.
type TaskListInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Path      string `json:"path,omitempty"`
	TaskCount int    `json:"task_count"`
	Active    bool   `json:"active"` // True when agent is actively working (.lock present)
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// TaskInfo represents a task for API responses.
type TaskInfo struct {
	ID          string         `json:"id"`
	ListID      string         `json:"list_id"`
	Subject     string         `json:"subject"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"` // pending | in_progress | completed
	ActiveForm  string         `json:"active_form,omitempty"`
	Owner       string         `json:"owner,omitempty"`
	Blocks      []string       `json:"blocks,omitempty"`
	BlockedBy   []string       `json:"blocked_by,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

// CreateTaskRequest is the request to create a new task.
type CreateTaskRequest struct {
	ListID      string `json:"list_id"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
}

// UpdateTaskRequest is the request to update a task.
type UpdateTaskRequest struct {
	ListID      string   `json:"list_id"`
	TaskID      string   `json:"task_id"`
	Subject     string   `json:"subject,omitempty"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	ActiveForm  string   `json:"active_form,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Blocks      []string `json:"blocks,omitempty"`
	BlockedBy   []string `json:"blocked_by,omitempty"`
}

// ExecuteTaskRequest is the request to execute a task with an agent.
type ExecuteTaskRequest struct {
	ListID       string `json:"list_id"`
	TaskID       string `json:"task_id"`
	WorktreePath string `json:"worktree_path,omitempty"` // Optional: use specific worktree
	Archetype    string `json:"archetype,omitempty"`     // Optional: agent archetype
}

// ListTaskProviders retrieves all registered task providers.
func (c *Client) ListTaskProviders() ([]string, error) {
	var providers []string
	if err := c.callAndDecode("list_task_providers", nil, &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

// ListTaskLists retrieves all task lists.
func (c *Client) ListTaskLists() ([]*TaskListInfo, error) {
	var lists []*TaskListInfo
	if err := c.callAndDecode("list_task_lists", nil, &lists); err != nil {
		return nil, err
	}
	return lists, nil
}

// ListTasks retrieves tasks from a list.
func (c *Client) ListTasks(listID string, status string) ([]*TaskInfo, error) {
	var tasks []*TaskInfo
	if err := c.callAndDecode("list_tasks", map[string]any{
		"list_id": listID,
		"status":  status,
	}, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetTask retrieves a specific task.
func (c *Client) GetTask(listID, taskID string) (*TaskInfo, error) {
	var task TaskInfo
	if err := c.callAndDecode("get_task", map[string]string{
		"list_id": listID,
		"task_id": taskID,
	}, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// CreateTask creates a new task in a list.
func (c *Client) CreateTask(req CreateTaskRequest) (*TaskInfo, error) {
	var task TaskInfo
	if err := c.callAndDecode("create_task", req, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateTask updates an existing task.
func (c *Client) UpdateTask(req UpdateTaskRequest) (*TaskInfo, error) {
	var task TaskInfo
	if err := c.callAndDecode("update_task", req, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// DeleteTask removes a task from a list.
func (c *Client) DeleteTask(listID, taskID string) error {
	resp, err := c.Call("delete_task", map[string]string{
		"list_id": listID,
		"task_id": taskID,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// ExecuteTask spawns an agent to execute a task.
func (c *Client) ExecuteTask(req ExecuteTaskRequest) (*AgentInfo, error) {
	var agent AgentInfo
	if err := c.callAndDecode("execute_task", req, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// BroadcastTask broadcasts a task to other sessions.
func (c *Client) BroadcastTask(listID, taskID string) error {
	resp, err := c.Call("broadcast_task", map[string]string{
		"list_id": listID,
		"task_id": taskID,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// SpawnChatRequest is the request to spawn an interactive chat session.
type SpawnChatRequest struct {
	WorktreePath string `json:"worktree_path"`
	Topic        string `json:"topic,omitempty"` // Optional initial topic for brainstorming
}

// SpawnChatResponse contains the command to run for the interactive session.
type SpawnChatResponse struct {
	AgentID   string   `json:"agent_id"`
	SessionID string   `json:"session_id"`
	Command   []string `json:"command"` // Command array to execute in terminal
	WorkDir   string   `json:"work_dir"`
}

// SpawnChat creates an interactive chat session for brainstorming.
// The returned command should be executed in a new terminal tab.
func (c *Client) SpawnChat(req SpawnChatRequest) (*SpawnChatResponse, error) {
	var result SpawnChatResponse
	if err := c.callAndDecode("spawn_chat", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// WorkItemInfo represents a hierarchical work item for API responses.
type WorkItemInfo struct {
	ID          string `json:"id"`          // wi-a3f8, wi-a3f8.1, etc.
	Project     string `json:"project"`     // project scope
	ItemType    string `json:"item_type"`   // goal, feature, task
	ParentID    string `json:"parent_id"`   // parent work item (empty for goals)
	Subject     string `json:"subject"`     // brief title
	Description string `json:"description"` // detailed description
	Status      string `json:"status"`      // pending, in_progress, completed
	Priority    int    `json:"priority"`    // 0=critical, 1=high, 2=normal, 3=low

	// Feature-specific
	WorktreePath string `json:"worktree_path,omitempty"` // linked worktree
	TicketID     string `json:"ticket_id,omitempty"`     // external ticket (ENG-123)
	PRURL        string `json:"pr_url,omitempty"`        // PR URL if published

	// Assignment
	AgentID string `json:"agent_id,omitempty"` // current agent

	// State
	Blocked bool `json:"blocked,omitempty"` // True when task has unresolved blockers

	// Progress (for goals/features)
	CompletedCount int `json:"completed_count,omitempty"` // completed descendants
	TotalCount     int `json:"total_count,omitempty"`     // total descendants

	// Timestamps
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListWorkItemsRequest is the request to list work items.
type ListWorkItemsRequest struct {
	Project  string `json:"project,omitempty"`
	ItemType string `json:"item_type,omitempty"` // goal, feature, task
	Status   string `json:"status,omitempty"`    // pending, in_progress, completed
}

// CreateWorkItemRequest is the request to create a work item.
type CreateWorkItemRequest struct {
	Project     string `json:"project"`
	ItemType    string `json:"item_type"`           // goal, feature, task
	ParentID    string `json:"parent_id,omitempty"` // parent (empty for goals/orphans)
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"` // defaults to pending
	TicketID    string `json:"ticket_id,omitempty"`
	Priority    int    `json:"priority,omitempty"` // defaults to 2 (normal)
}

// UpdateWorkItemRequest is the request to update a work item.
type UpdateWorkItemRequest struct {
	ID          string `json:"id"`
	Subject     string `json:"subject,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
}

// ListWorkItems retrieves work items with optional filters.
func (c *Client) ListWorkItems(req ListWorkItemsRequest) ([]*WorkItemInfo, error) {
	var items []*WorkItemInfo
	if err := c.callAndDecode("list_work_items", req, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetWorkItem retrieves a work item by ID.
func (c *Client) GetWorkItem(id string) (*WorkItemInfo, error) {
	var item WorkItemInfo
	if err := c.callAndDecode("get_work_item", map[string]string{"id": id}, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateWorkItem creates a new work item.
func (c *Client) CreateWorkItem(req CreateWorkItemRequest) (*WorkItemInfo, error) {
	var item WorkItemInfo
	if err := c.callAndDecode("create_work_item", req, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateWorkItem updates an existing work item.
func (c *Client) UpdateWorkItem(req UpdateWorkItemRequest) (*WorkItemInfo, error) {
	var item WorkItemInfo
	if err := c.callAndDecode("update_work_item", req, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// DeleteWorkItem removes a work item.
func (c *Client) DeleteWorkItem(id string) error {
	resp, err := c.Call("delete_work_item", map[string]string{"id": id})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// GetWorkItemTree retrieves a work item and all its descendants.
func (c *Client) GetWorkItemTree(rootID, project string) ([]*WorkItemInfo, error) {
	var items []*WorkItemInfo
	if err := c.callAndDecode("get_work_item_tree", map[string]string{
		"root_id": rootID,
		"project": project,
	}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetWorkItemChildren retrieves direct children of a work item.
func (c *Client) GetWorkItemChildren(parentID string) ([]*WorkItemInfo, error) {
	var items []*WorkItemInfo
	if err := c.callAndDecode("get_work_item_children", map[string]string{"parent_id": parentID}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetWorkItemAncestors retrieves all ancestors of a work item.
func (c *Client) GetWorkItemAncestors(id string) ([]*WorkItemInfo, error) {
	var items []*WorkItemInfo
	if err := c.callAndDecode("get_work_item_ancestors", map[string]string{"id": id}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetReadyItems retrieves unblocked work items ready to be worked on.
func (c *Client) GetReadyItems(project string) ([]*WorkItemInfo, error) {
	var items []*WorkItemInfo
	if err := c.callAndDecode("get_ready_items", map[string]string{"project": project}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// ============================================================================
// Merge Queue API
// ============================================================================

// MergeQueueItemInfo represents a worktree in the merge queue.
type MergeQueueItemInfo struct {
	ID           string `json:"id"`
	Project      string `json:"project"`
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
	Position     int    `json:"position"`
	Status       string `json:"status"` // queued, merging, merged, conflict, rebasing, diverged
	BaseBranch   string `json:"base_branch"`
	BaseCommit   string `json:"base_commit"`
	HeadCommit   string `json:"head_commit,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// MergeQueueHeadInfo represents the current integration HEAD.
type MergeQueueHeadInfo struct {
	Branch string `json:"branch"` // Empty means use main
	Commit string `json:"commit"` // Empty means use main HEAD
	Empty  bool   `json:"empty"`  // True if queue is empty (use main)
}

// AddToMergeQueueRequest is the request to add a worktree to the merge queue.
type AddToMergeQueueRequest struct {
	WorktreePath string `json:"worktree_path"`
	Project      string `json:"project,omitempty"` // Auto-detected if not provided
}

// GetMergeQueue retrieves all items in the merge queue for a project.
func (c *Client) GetMergeQueue(project string) ([]*MergeQueueItemInfo, error) {
	var items []*MergeQueueItemInfo
	if err := c.callAndDecode("get_merge_queue", map[string]string{"project": project}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetMergeQueueHead returns the integration HEAD - what new worktrees should base on.
func (c *Client) GetMergeQueueHead(project string) (*MergeQueueHeadInfo, error) {
	var head MergeQueueHeadInfo
	if err := c.callAndDecode("get_merge_queue_head", map[string]string{"project": project}, &head); err != nil {
		return nil, err
	}
	return &head, nil
}

// AddToMergeQueue adds a worktree to the merge queue.
func (c *Client) AddToMergeQueue(req AddToMergeQueueRequest) (*MergeQueueItemInfo, error) {
	var item MergeQueueItemInfo
	if err := c.callAndDecode("add_to_merge_queue", req, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// RemoveFromMergeQueue removes a worktree from the merge queue.
func (c *Client) RemoveFromMergeQueue(worktreePath string) error {
	resp, err := c.Call("remove_from_merge_queue", map[string]string{"worktree_path": worktreePath})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// BumpMergeQueueItem moves a worktree to the back of the queue (after editing).
func (c *Client) BumpMergeQueueItem(worktreePath string) (*MergeQueueItemInfo, error) {
	var item MergeQueueItemInfo
	if err := c.callAndDecode("bump_merge_queue_item", map[string]string{"worktree_path": worktreePath}, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// RebaseMergeQueueItem marks a queue item as rebased with new commits.
func (c *Client) RebaseMergeQueueItem(worktreePath, newBaseCommit, newHeadCommit string) error {
	resp, err := c.Call("rebase_merge_queue_item", map[string]string{
		"worktree_path":   worktreePath,
		"new_base_commit": newBaseCommit,
		"new_head_commit": newHeadCommit,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// ReconcileQueueResult contains the results of reconciling diverged queue items.
type ReconcileQueueResult struct {
	Results []map[string]string `json:"results"`
}

// ReconcileQueue refreshes the queue graph and rebases any diverged items.
func (c *Client) ReconcileQueue(project string) (*ReconcileQueueResult, error) {
	var result ReconcileQueueResult
	if err := c.callAndDecode("reconcile_queue", map[string]string{"project": project}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PruneWorktreesResult contains the results of pruning worktrees.
type PruneWorktreesResult struct {
	Merged  []string `json:"merged"`  // Merged worktrees that were pruned
	Orphans []string `json:"orphans"` // Orphaned directories that were removed
	Stale   []string `json:"stale"`   // Stale database entries that were removed
}

// SpawnRequest is the request for the unified spawn command.
type SpawnRequest struct {
	FeatureID  string `json:"feature_id,omitempty"`   // e.g. wi-a3f8.1 (primary flow: spawn on feature)
	TicketID   string `json:"ticket_id,omitempty"`    // e.g. ENG-123
	WorkItemID string `json:"work_item_id,omitempty"` // e.g. wi-a3f8
	Goal       string `json:"goal,omitempty"`         // Free-form goal text (e.g. "implement user auth")
	Project    string `json:"project,omitempty"`
	Retrieve   bool   `json:"retrieve,omitempty"`  // -r flag: break down goal first
	Headless   bool   `json:"headless,omitempty"`  // --headless flag: run detached
	Worktree   bool   `json:"worktree,omitempty"`  // -w flag: create worktree
	Parallel   bool   `json:"parallel,omitempty"`  // -p flag: parallel task-worker mode
	WorkDir    string `json:"work_dir,omitempty"`  // current dir for bare mode
	Archetype  string `json:"archetype,omitempty"` // Explicit archetype override (e.g. "reconciler")
}

// SpawnResponse contains the result of a spawn request.
type SpawnResponse struct {
	Agent      *AgentInfo    `json:"agent,omitempty"`     // set for headless spawns
	WorkItem   *WorkItemInfo `json:"work_item"`           // always set
	Worktree   *WorktreeInfo `json:"worktree,omitempty"`  // set if worktree created
	TaskListID string        `json:"task_list_id"`        // work item ID used as task list
	ExecArgs   []string      `json:"exec_args,omitempty"` // set for interactive spawns
	ExecEnv    []string      `json:"exec_env,omitempty"`  // env vars for interactive exec
}

// Spawn initiates the unified spawn flow.
func (c *Client) Spawn(req SpawnRequest) (*SpawnResponse, error) {
	var result SpawnResponse
	if err := c.callAndDecode("spawn", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PruneWorktrees cleans up merged and orphaned worktrees.
func (c *Client) PruneWorktrees() (*PruneWorktreesResult, error) {
	var result PruneWorktreesResult
	if err := c.callAndDecode("prune_worktrees", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RateLimitStatus contains the current rate limit state.
type RateLimitStatus struct {
	Limited    bool   `json:"limited"`
	ResetAt    string `json:"reset_at,omitempty"` // RFC 3339
	Reason     string `json:"reason,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`    // which agent triggered it
	HitCount   int    `json:"hit_count"`             // total hits this session
	WaitingSec int    `json:"waiting_sec,omitempty"` // seconds until reset
}

// GetRateLimitStatus returns the current rate limit status.
func (c *Client) GetRateLimitStatus() (*RateLimitStatus, error) {
	var result RateLimitStatus
	if err := c.callAndDecode("rate_limit_status", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AutoRunRequest starts the auto-run loop.
type AutoRunRequest struct {
	Project string `json:"project,omitempty"`
	Once    bool   `json:"once,omitempty"` // Run one task and stop
}

// AutoRunResult tracks the outcome of a single auto-run item.
type AutoRunResult struct {
	ItemID   string `json:"item_id"`
	Subject  string `json:"subject"`
	ItemType string `json:"item_type"`
	AgentID  string `json:"agent_id,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"` // spawn error or termination reason
}

// AutoRunStatus contains the current state of the auto-run loop.
type AutoRunStatus struct {
	Running        bool            `json:"running"`
	Project        string          `json:"project"`
	CurrentItem    *WorkItemInfo   `json:"current_item,omitempty"`
	CurrentAgent   *AgentInfo      `json:"current_agent,omitempty"`
	Completed      int             `json:"completed"`
	Failed         int             `json:"failed"`
	CompletedItems []AutoRunResult `json:"completed_items,omitempty"`
	FailedItems    []AutoRunResult `json:"failed_items,omitempty"`
}

// StartAutoRun starts the auto-run loop on the daemon.
func (c *Client) StartAutoRun(req AutoRunRequest) (*AutoRunStatus, error) {
	var result AutoRunStatus
	if err := c.callAndDecode("start_auto_run", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// StopAutoRun stops the auto-run loop.
func (c *Client) StopAutoRun() (*AutoRunStatus, error) {
	var result AutoRunStatus
	if err := c.callAndDecode("stop_auto_run", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAutoRunStatus returns the current auto-run status.
func (c *Client) GetAutoRunStatus() (*AutoRunStatus, error) {
	var result AutoRunStatus
	if err := c.callAndDecode("auto_run_status", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
