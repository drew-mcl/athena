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

func (s *Store) migrate() error {
	// First, run any ALTER TABLE migrations for existing databases
	if err := s.migrateWorktreeColumns(); err != nil {
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
	TicketID    *string        // External ticket ID (e.g., ENG-123)
	TicketHash  *string        // 4-char hash for uniqueness
	Description *string        // Worktree description/purpose
	ProjectName *string        // Cached from git remote origin
	Status      WorktreeStatus // active | published | merged | stale
	PRURL       *string        // GitHub PR URL if published via PR flow
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
	Status       PlanStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AgentMetrics holds usage statistics for an agent.
type AgentMetrics struct {
	ToolUseCount int
	FilesRead    int
	FilesWritten int
	LinesChanged int
	MessageCount int
	Duration     time.Duration
}
