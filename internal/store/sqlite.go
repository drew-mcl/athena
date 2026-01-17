// Package store provides SQLite-backed persistence for Athena state.
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

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

func (s *Store) migrate() error {
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

	-- Worktree registry
	CREATE TABLE IF NOT EXISTS worktrees (
		path        TEXT PRIMARY KEY,
		project     TEXT NOT NULL,
		branch      TEXT,
		is_main     BOOLEAN DEFAULT FALSE,
		agent_id    TEXT,
		job_id      TEXT,
		discovered_at DATETIME DEFAULT CURRENT_TIMESTAMP,

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

	-- Indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
	CREATE INDEX IF NOT EXISTS idx_agents_worktree ON agents(worktree_path);
	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_events_agent ON agent_events(agent_id, timestamp);
	CREATE INDEX IF NOT EXISTS idx_worktrees_project ON worktrees(project);
	`

	_, err := s.db.Exec(schema)
	return err
}

// AgentStatus represents the lifecycle state of an agent.
type AgentStatus string

const (
	AgentStatusPending    AgentStatus = "pending"
	AgentStatusSpawning   AgentStatus = "spawning"
	AgentStatusRunning    AgentStatus = "running"
	AgentStatusPlanning   AgentStatus = "planning"
	AgentStatusExecuting  AgentStatus = "executing"
	AgentStatusAwaiting   AgentStatus = "awaiting"
	AgentStatusCrashed    AgentStatus = "crashed"
	AgentStatusCompleted  AgentStatus = "completed"
	AgentStatusTerminated AgentStatus = "terminated"
	AgentStatusAttached   AgentStatus = "attached"
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

// Worktree represents a git worktree.
type Worktree struct {
	Path         string
	Project      string
	Branch       string
	IsMain       bool
	AgentID      *string
	JobID        *string
	DiscoveredAt time.Time
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
