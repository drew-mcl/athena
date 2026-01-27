// Package store provides SQLite-backed persistence for Athena state.
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ContextCacheMetric tracks context cache performance for cross-agent analysis.
type ContextCacheMetric struct {
	ID                       string
	AgentID                  string
	ProjectName              string
	WorktreePath             string
	StateEntriesCount        int
	StateTokensEstimate      int
	BlackboardEntriesCount   int
	BlackboardTokensEstimate int
	CacheReadsTotal          int
	CacheReadsInState        int
	CacheReadsInBlackboard   int
	TotalContextTokens       int
	CacheHitRate             float64
	IsFirstAgent             bool
	CreatedAt                time.Time
}

// ProjectCacheStats provides aggregated cache statistics for a project.
type ProjectCacheStats struct {
	ProjectName         string
	TotalAgents         int
	FirstAgentCount     int
	SubsequentAgentCount int
	AvgCacheHitRate     float64
	AvgFirstAgentCacheRate   float64
	AvgSubsequentAgentCacheRate float64
	TotalStateTokens    int
	TotalBlackboardTokens int
	TotalCacheReads     int
}

// Store is the main persistence layer for Athena.
type Store struct {
	db *sql.DB
}

// New creates a new Store, initializing the database if needed.
func New(dbPath string) (*Store, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrateWorktreeColumns adds new columns to the worktrees table if they don't exist.
// This handles schema upgrades for existing databases.
func (s *Store) migrateWorktreeColumns() error {
	// Check if worktrees table exists
	var tableName string
	err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='worktrees'").Scan(&tableName)
	if err != nil {
		// Table doesn't exist yet, will be created by main schema
		return nil
	}

	// Get existing columns
	rows, err := s.db.Query("PRAGMA table_info(worktrees)")
	if err != nil {
		return err
	}
	defer rows.Close()

	existingCols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var defaultVal *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		existingCols[name] = true
	}

	// Define new columns that may need to be added
	newColumns := []struct {
		name       string
		definition string
	}{
		{"ticket_id", "TEXT"},
		{"ticket_hash", "TEXT"},
		{"description", "TEXT"},
		{"project_name", "TEXT"},
		{"status", "TEXT DEFAULT 'active'"},
		{"pr_url", "TEXT"},
		{"workflow_mode", "TEXT DEFAULT 'approve'"},
		{"source_note_id", "TEXT"}, // ID of the note that created this worktree (for abandon rollback)
	}

	// Add missing columns
	for _, col := range newColumns {
		if !existingCols[col.name] {
			query := "ALTER TABLE worktrees ADD COLUMN " + col.name + " " + col.definition
			if _, err := s.db.Exec(query); err != nil {
				return err
			}
		}
	}

	return nil
}

// migratePlanColumns adds new columns to the plans table if they don't exist.
func (s *Store) migratePlanColumns() error {
	// Check if plans table exists
	var tableName string
	err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='plans'").Scan(&tableName)
	if err != nil {
		// Table doesn't exist yet, will be created by main schema
		return nil
	}

	// Get existing columns
	rows, err := s.db.Query("PRAGMA table_info(plans)")
	if err != nil {
		return err
	}
	defer rows.Close()

	existingCols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var defaultVal *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		existingCols[name] = true
	}

	// Add summary column if missing
	if !existingCols["summary"] {
		if _, err := s.db.Exec("ALTER TABLE plans ADD COLUMN summary TEXT"); err != nil {
			return err
		}
	}

	return nil
}

// migrateMetricsColumns adds new columns to the agent_metrics table if they don't exist.
func (s *Store) migrateMetricsColumns() error {
	// Check if agent_metrics table exists
	var tableName string
	err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='agent_metrics'").Scan(&tableName)
	if err != nil {
		// Table doesn't exist yet, will be created by main schema
		return nil
	}

	// Get existing columns
	rows, err := s.db.Query("PRAGMA table_info(agent_metrics)")
	if err != nil {
		return err
	}
	defer rows.Close()

	existingCols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var defaultVal *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		existingCols[name] = true
	}

	// Add tool_failures column if missing
	if !existingCols["tool_failures"] {
		if _, err := s.db.Exec("ALTER TABLE agent_metrics ADD COLUMN tool_failures INTEGER DEFAULT 0"); err != nil {
			return err
		}
	}

	return nil
}

// migrateContextCacheMetrics creates the context_cache_metrics table if it doesn't exist.
// This table is created via migration rather than main schema to support existing databases.
func (s *Store) migrateContextCacheMetrics() error {
	schema := `
	CREATE TABLE IF NOT EXISTS context_cache_metrics (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		project_name TEXT NOT NULL,
		worktree_path TEXT,

		-- Context section sizes
		state_entries_count INT DEFAULT 0,
		state_tokens_estimate INT DEFAULT 0,
		blackboard_entries_count INT DEFAULT 0,
		blackboard_tokens_estimate INT DEFAULT 0,

		-- Cache performance (attributed)
		cache_reads_total INT DEFAULT 0,
		cache_reads_in_state INT DEFAULT 0,
		cache_reads_in_blackboard INT DEFAULT 0,

		-- Overall metrics
		total_context_tokens INT DEFAULT 0,
		cache_hit_rate REAL DEFAULT 0,

		-- Tracking
		is_first_agent BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);
	CREATE INDEX IF NOT EXISTS idx_cache_project ON context_cache_metrics(project_name);
	CREATE INDEX IF NOT EXISTS idx_cache_agent ON context_cache_metrics(agent_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) migrate() error {
	// First, run any ALTER TABLE migrations for existing databases
	if err := s.migrateWorktreeColumns(); err != nil {
		return err
	}
	if err := s.migratePlanColumns(); err != nil {
		return err
	}
	if err := s.migrateMetricsColumns(); err != nil {
		return err
	}
	if err := s.migrateContextCacheMetrics(); err != nil {
		return err
	}

	schema := `
	-- Core agent registry
	CREATE TABLE IF NOT EXISTS agents (
		id              TEXT PRIMARY KEY,
		worktree_path   TEXT NOT NULL,
		project_name    TEXT NOT NULL,
		archetype       TEXT NOT NULL,
		status          TEXT NOT NULL,
		pid             INTEGER,
		exit_code       INTEGER,
		restart_count   INTEGER DEFAULT 0,
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,

		-- Task context
		prompt          TEXT,
		linear_issue_id TEXT,
		parent_agent_id TEXT,

		-- Session tracking
		claude_session_id TEXT,
		last_heartbeat  DATETIME,

		FOREIGN KEY (parent_agent_id) REFERENCES agents(id)
	);

	-- Jobs (user-facing task wrapper)
	CREATE TABLE IF NOT EXISTS jobs (
		id                TEXT PRIMARY KEY,
		raw_input         TEXT NOT NULL,
		normalized_input  TEXT NOT NULL,
		status            TEXT NOT NULL,
		job_type          TEXT NOT NULL DEFAULT 'feature',
		project           TEXT NOT NULL DEFAULT '',
		created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP,

		-- External links
		external_id       TEXT,
		external_url      TEXT,

		-- Agent relationships
		current_agent_id  TEXT,
		agent_history     TEXT,

		-- Quick job fields
		target_branch     TEXT,           -- Branch to merge into (for quick jobs, default: main)
		worktree_path     TEXT,           -- Temp worktree path (for quick jobs)
		commit_hash       TEXT,           -- Commit SHA created by quick job
		answer            TEXT,           -- Response text (for question jobs)
		propagation_results TEXT,         -- JSON: merge status per worktree for broadcast

		FOREIGN KEY (current_agent_id) REFERENCES agents(id)
	);

	-- Event log for debugging/replay
	CREATE TABLE IF NOT EXISTS agent_events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		agent_id   TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload    TEXT,
		timestamp  DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);

	-- Messages (eventlog-backed context and replay)
	CREATE TABLE IF NOT EXISTS messages (
		id            TEXT PRIMARY KEY,
		agent_id      TEXT NOT NULL,
		direction     TEXT NOT NULL,
		type          TEXT NOT NULL,
		sequence      INTEGER NOT NULL,
		timestamp     DATETIME DEFAULT CURRENT_TIMESTAMP,
		text          TEXT,
		tool_name     TEXT,
		tool_input    TEXT,
		tool_output   TEXT,
		error_code    TEXT,
		error_message TEXT,
		session_id    TEXT,
		raw           TEXT,

		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);

	-- Worktree registry
	CREATE TABLE IF NOT EXISTS worktrees (
		path        TEXT PRIMARY KEY,
		project     TEXT NOT NULL,
		branch      TEXT,
		is_main     BOOLEAN DEFAULT FALSE,
		agent_id    TEXT,
		job_id      TEXT,
		discovered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		-- New fields for ticket-based workflow
		ticket_id     TEXT,                      -- External ticket ID (e.g., ENG-123)
		ticket_hash   TEXT,                      -- 4-char hash for uniqueness
		description   TEXT,                      -- Worktree description/purpose
		project_name  TEXT,                      -- Cached from git remote origin
		status        TEXT DEFAULT 'active',     -- active | published | merged | stale
		pr_url        TEXT,                      -- GitHub PR URL if published via PR flow
		workflow_mode TEXT DEFAULT 'approve',    -- automatic | approve | manual
		source_note_id TEXT,                     -- ID of the note that created this worktree (for abandon rollback)

		FOREIGN KEY (agent_id) REFERENCES agents(id),
		FOREIGN KEY (job_id) REFERENCES jobs(id)
	);

	-- Notes (quick ideas/todos)
	CREATE TABLE IF NOT EXISTS notes (
		id         TEXT PRIMARY KEY,
		content    TEXT NOT NULL,
		done       BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Changelog (track completed work for history/memory)
	CREATE TABLE IF NOT EXISTS changelog (
		id          TEXT PRIMARY KEY,
		title       TEXT NOT NULL,
		description TEXT,
		category    TEXT NOT NULL DEFAULT 'feature',  -- feature | fix | refactor | docs
		project     TEXT,
		job_id      TEXT,
		agent_id    TEXT,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (job_id) REFERENCES jobs(id),
		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);

	-- Plans table for storing implementation plans created by planner agents
	CREATE TABLE IF NOT EXISTS plans (
		id            TEXT PRIMARY KEY,
		worktree_path TEXT NOT NULL,
		agent_id      TEXT NOT NULL,              -- planner agent that created it
		content       TEXT NOT NULL,              -- markdown content
		summary       TEXT,                       -- brief summary from frontmatter
		status        TEXT DEFAULT 'draft',       -- draft | approved | executing | completed
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (worktree_path) REFERENCES worktrees(path),
		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);

	-- Indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
	CREATE INDEX IF NOT EXISTS idx_agents_worktree ON agents(worktree_path);
	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_events_agent ON agent_events(agent_id, timestamp);
	CREATE INDEX IF NOT EXISTS idx_messages_agent_seq ON messages(agent_id, sequence);
	CREATE INDEX IF NOT EXISTS idx_messages_agent_type ON messages(agent_id, type);
	CREATE INDEX IF NOT EXISTS idx_worktrees_project ON worktrees(project);
	CREATE INDEX IF NOT EXISTS idx_worktrees_ticket ON worktrees(ticket_id);
	CREATE INDEX IF NOT EXISTS idx_worktrees_status ON worktrees(status);
	CREATE INDEX IF NOT EXISTS idx_worktrees_project_name ON worktrees(project_name);
	CREATE INDEX IF NOT EXISTS idx_plans_worktree ON plans(worktree_path);
	CREATE INDEX IF NOT EXISTS idx_plans_agent ON plans(agent_id);

	-- Blackboard: ephemeral, worktree-scoped entries for current workflow
	CREATE TABLE IF NOT EXISTS blackboard (
		id            TEXT PRIMARY KEY,
		worktree_path TEXT NOT NULL,
		entry_type    TEXT NOT NULL,              -- decision | finding | attempt | question | artifact
		content       TEXT NOT NULL,
		agent_id      TEXT NOT NULL,
		sequence      INTEGER NOT NULL,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		resolved      INTEGER DEFAULT 0,
		resolved_by   TEXT,

		FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL
	);
	CREATE INDEX IF NOT EXISTS idx_blackboard_worktree ON blackboard(worktree_path, sequence);
	CREATE INDEX IF NOT EXISTS idx_blackboard_type ON blackboard(worktree_path, entry_type);

	-- Project state: durable, project-scoped facts that survive across workflows
	CREATE TABLE IF NOT EXISTS project_state (
		id            TEXT PRIMARY KEY,
		project       TEXT NOT NULL,
		state_type    TEXT NOT NULL,              -- architecture | convention | constraint | decision | environment
		key           TEXT NOT NULL,
		value         TEXT NOT NULL,
		confidence    REAL DEFAULT 1.0,
		source_agent  TEXT,
		source_ref    TEXT,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		superseded_by TEXT,

		UNIQUE(project, state_type, key),
		FOREIGN KEY (source_agent) REFERENCES agents(id) ON DELETE SET NULL,
		FOREIGN KEY (superseded_by) REFERENCES project_state(id)
	);
	CREATE INDEX IF NOT EXISTS idx_state_project ON project_state(project, state_type);

	-- Snapshots: conversation checkpoints for agent recovery
	CREATE TABLE IF NOT EXISTS snapshots (
		id            TEXT PRIMARY KEY,
		agent_id      TEXT NOT NULL,
		sequence      INTEGER NOT NULL,
		timestamp     DATETIME NOT NULL,
		checksum      TEXT NOT NULL,
		data          TEXT NOT NULL,              -- JSON serialized conversation
		message_count INTEGER NOT NULL,
		tool_call_count INTEGER NOT NULL,
		duration_ms   INTEGER NOT NULL,
		is_complete   BOOLEAN DEFAULT FALSE,
		metadata      TEXT,                      -- JSON metadata
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);
	CREATE INDEX IF NOT EXISTS idx_snapshots_agent ON snapshots(agent_id, sequence DESC);

	-- Agent metrics for usage tracking
	CREATE TABLE IF NOT EXISTS agent_metrics (
		id                    TEXT PRIMARY KEY,
		agent_id              TEXT NOT NULL,
		session_id            TEXT NOT NULL,

		-- Token usage
		input_tokens          INTEGER DEFAULT 0,
		output_tokens         INTEGER DEFAULT 0,
		cache_read_tokens     INTEGER DEFAULT 0,
		cache_creation_tokens INTEGER DEFAULT 0,

		-- Timing
		duration_ms           INTEGER DEFAULT 0,
		api_time_ms           INTEGER DEFAULT 0,

		-- Cost (in cents)
		cost_cents            INTEGER DEFAULT 0,

		-- Activity
		num_turns             INTEGER DEFAULT 0,
		tool_calls            INTEGER DEFAULT 0,
		tool_successes        INTEGER DEFAULT 0,
		tool_failures         INTEGER DEFAULT 0,

		-- Timestamps
		started_at            DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at          DATETIME,

		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);
	CREATE INDEX IF NOT EXISTS idx_metrics_agent ON agent_metrics(agent_id);
	CREATE INDEX IF NOT EXISTS idx_metrics_session ON agent_metrics(session_id);

	-- Symbol index (rebuilt on change)
	CREATE TABLE IF NOT EXISTS symbols (
		project_hash  TEXT NOT NULL,
		symbol_name   TEXT NOT NULL,
		kind          TEXT NOT NULL,           -- func, struct, interface, const, var, method, type
		file_path     TEXT NOT NULL,
		line_number   INTEGER NOT NULL,
		package       TEXT,
		receiver      TEXT,
		exported      BOOLEAN DEFAULT FALSE,
		PRIMARY KEY (project_hash, symbol_name, file_path, line_number)
	);
	CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(symbol_name);
	CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(project_hash, file_path);
	CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(project_hash, kind);

	-- Dependency graph
	CREATE TABLE IF NOT EXISTS dependencies (
		project_hash  TEXT NOT NULL,
		from_file     TEXT NOT NULL,
		to_file       TEXT NOT NULL,
		dep_type      TEXT NOT NULL DEFAULT 'import',   -- import
		import_path   TEXT,
		is_internal   BOOLEAN DEFAULT FALSE,
		PRIMARY KEY (project_hash, from_file, to_file, import_path)
	);
	CREATE INDEX IF NOT EXISTS idx_deps_from ON dependencies(project_hash, from_file);
	CREATE INDEX IF NOT EXISTS idx_deps_to ON dependencies(project_hash, to_file);
	CREATE INDEX IF NOT EXISTS idx_deps_internal ON dependencies(project_hash, is_internal);

	-- File access tracking for agents
	CREATE TABLE IF NOT EXISTS file_access (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		access_count INTEGER DEFAULT 1,
		tokens_consumed INTEGER DEFAULT 0,
		first_access_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_access_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(agent_id, file_path)
	);
	CREATE INDEX IF NOT EXISTS idx_file_access_agent ON file_access(agent_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// AgentStatus represents the lifecycle state of an agent.
type AgentStatus string

const (
	AgentStatusPending     AgentStatus = "pending"
	AgentStatusSpawning    AgentStatus = "spawning"
	AgentStatusRunning     AgentStatus = "running"
	AgentStatusPlanning    AgentStatus = "planning"
	AgentStatusExecuting   AgentStatus = "executing"
	AgentStatusAwaiting    AgentStatus = "awaiting"
	AgentStatusCrashed     AgentStatus = "crashed"
	AgentStatusCompleted   AgentStatus = "completed"
	AgentStatusTerminated  AgentStatus = "terminated"
	AgentStatusAttached    AgentStatus = "attached"
	AgentStatusInteractive AgentStatus = "interactive" // Interactive chat session
)

// Agent represents a Claude Code agent instance.
type Agent struct {
	ID              string
	WorktreePath    string
	ProjectName     string
	Archetype       string
	Status          AgentStatus
	PID             *int
	ExitCode        *int
	RestartCount    int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Prompt          string
	LinearIssueID   *string
	ParentAgentID   *string
	ClaudeSessionID string
	LastHeartbeat   *time.Time
}

// JobStatus represents the state of a user-facing job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusPlanning  JobStatus = "planning"
	JobStatusExecuting JobStatus = "executing"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusConflict  JobStatus = "conflict" // Merge conflict needs resolution
)

// JobType represents the kind of job.
type JobType string

const (
	JobTypeQuestion JobType = "question" // No code change, just Q&A (no worktree)
	JobTypeQuick    JobType = "quick"    // Fast change, broadcast to all worktrees
	JobTypeFeature  JobType = "feature"  // Long-lived feature work (agent-based)
	JobTypeMerge    JobType = "merge"    // Merge conflict resolution
)

// PropagationResult tracks the merge status for each worktree during quick job broadcast.
type PropagationResult struct {
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
	Status       string `json:"status"` // pending | merged | conflict | skipped | notified
	Error        string `json:"error,omitempty"`
	AgentID      string `json:"agent_id,omitempty"` // If agent was notified instead of auto-merge
}

// Job represents a user-submitted task.
type Job struct {
	ID              string
	RawInput        string
	NormalizedInput string
	Status          JobStatus
	Type            JobType
	Project         string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ExternalID      *string
	ExternalURL     *string
	CurrentAgentID  *string
	AgentHistory    []string
	// Quick job fields
	TargetBranch *string // Branch to merge into (for quick jobs, default: main)
	WorktreePath *string // Temp worktree path (for quick jobs)
	CommitHash   *string // Commit SHA created by quick job
	Answer       *string // Response text (for question jobs)
	// Propagation tracking (quick jobs broadcast to all worktrees)
	PropagationResults []PropagationResult // Status per worktree
}

// WorktreeStatus represents the lifecycle state of a worktree.
type WorktreeStatus string

const (
	WorktreeStatusActive    WorktreeStatus = "active"
	WorktreeStatusPublished WorktreeStatus = "published" // PR created, awaiting merge
	WorktreeStatusMerged    WorktreeStatus = "merged"
	WorktreeStatusStale     WorktreeStatus = "stale"
)

// Worktree represents a git worktree.
type Worktree struct {
	Path         string
	Project      string
	Branch       string
	IsMain       bool
	AgentID      *string
	JobID        *string
	DiscoveredAt time.Time
	// New fields for ticket-based workflow
	TicketID     *string        // External ticket ID (e.g., ENG-123)
	TicketHash   *string        // 4-char hash for uniqueness
	Description  *string        // Worktree description/purpose
	ProjectName  *string        // Cached from git remote origin
	Status       WorktreeStatus // active | published | merged | stale
	PRURL        *string        // GitHub PR URL if published via PR flow
	WorkflowMode *string        // automatic | approve | manual
	SourceNoteID *string        // ID of the note that created this worktree (for abandon rollback)
}

// AgentEvent represents a logged event from an agent.
type AgentEvent struct {
	ID        int64
	AgentID   string
	EventType string
	Payload   string
	Timestamp time.Time
}

// Note represents a quick note/idea.
type Note struct {
	ID        string
	Content   string
	Done      bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChangelogEntry represents a completed work item.
type ChangelogEntry struct {
	ID          string
	Title       string
	Description string
	Category    string // feature | fix | refactor | docs
	Project     string
	JobID       *string
	AgentID     *string
	CreatedAt   time.Time
}

// BlackboardEntryType represents the kind of blackboard entry.
type BlackboardEntryType string

const (
	BlackboardTypeDecision BlackboardEntryType = "decision" // Choice made
	BlackboardTypeFinding  BlackboardEntryType = "finding"  // Discovery
	BlackboardTypeAttempt  BlackboardEntryType = "attempt"  // What was tried
	BlackboardTypeQuestion BlackboardEntryType = "question" // Open question
	BlackboardTypeArtifact BlackboardEntryType = "artifact" // Created thing
)

// BlackboardEntry represents an ephemeral, worktree-scoped entry for the current workflow.
type BlackboardEntry struct {
	ID           string
	WorktreePath string
	EntryType    BlackboardEntryType
	Content      string
	AgentID      string
	Sequence     int
	CreatedAt    time.Time
	Resolved     bool
	ResolvedBy   *string
}

// StateEntryType represents the kind of project state entry.
type StateEntryType string

const (
	StateTypeArchitecture StateEntryType = "architecture" // Design patterns
	StateTypeConvention   StateEntryType = "convention"   // Code style
	StateTypeConstraint   StateEntryType = "constraint"   // Hard limits
	StateTypeDecision     StateEntryType = "decision"     // Architectural choice
	StateTypeEnvironment  StateEntryType = "environment"  // Runtime facts
)

// StateEntry represents a durable, project-scoped fact that survives across workflows.
type StateEntry struct {
	ID           string
	Project      string
	StateType    StateEntryType
	Key          string
	Value        string
	Confidence   float64
	SourceAgent  *string
	SourceRef    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	SupersededBy *string
}

// PlanStatus represents the lifecycle state of a plan.
type PlanStatus string

const (
	PlanStatusPending   PlanStatus = "pending"   // Planner agent still working, no .plan.md yet
	PlanStatusDraft     PlanStatus = "draft"     // .plan.md exists, awaiting approval
	PlanStatusApproved  PlanStatus = "approved"  // User approved, ready for executor
	PlanStatusExecuting PlanStatus = "executing" // Executor agent running
	PlanStatusCompleted PlanStatus = "completed" // Executor finished
)

// Plan represents an implementation plan created by a planner agent.
type Plan struct {
	ID           string
	WorktreePath string
	AgentID      string // planner agent that created it
	Content      string // markdown content
	Summary      string // brief summary extracted from frontmatter
	Status       PlanStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AgentMetrics holds usage statistics for an agent session.
type AgentMetrics struct {
	ID        string
	AgentID   string
	SessionID string

	// Token usage
	InputTokens        int
	OutputTokens       int
	CacheReadTokens    int
	CacheCreationTokens int

	// Timing
	DurationMS int64
	APITimeMS  int64

	// Cost
	CostCents int // Stored as cents to avoid float precision issues

	// Activity
	NumTurns      int
	ToolCalls     int
	ToolSuccesses int
	ToolFailures  int

	// Timestamps
	StartedAt   time.Time
	CompletedAt *time.Time
}

// CacheHitRate returns the percentage of input tokens served from cache.
func (m *AgentMetrics) CacheHitRate() float64 {
	total := m.InputTokens + m.CacheReadTokens + m.CacheCreationTokens
	if total == 0 {
		return 0
	}
	return float64(m.CacheReadTokens) / float64(total) * 100
}

// TotalInputTokens returns the sum of all input tokens (fresh + cached).
func (m *AgentMetrics) TotalInputTokens() int {
	return m.InputTokens + m.CacheReadTokens + m.CacheCreationTokens
}

// CostUSD returns the cost in USD.
func (m *AgentMetrics) CostUSD() float64 {
	return float64(m.CostCents) / 100
}
