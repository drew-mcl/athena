// Package task provides abstractions for task management across different providers.
package task

import (
	"context"
	"time"
)

// Status represents the status of a task.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// TaskList represents a scoped collection of tasks.
// In Claude Code, this maps to CLAUDE_CODE_TASK_LIST_ID.
type TaskList struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Provider    string    `json:"provider"`
	Path        string    `json:"path,omitempty"` // File path for file-based providers
	TaskCount   int       `json:"task_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Task represents a single task item.
type Task struct {
	ID          string            `json:"id"`
	ListID      string            `json:"list_id"`
	Subject     string            `json:"subject"`
	Description string            `json:"description,omitempty"`
	Status      Status            `json:"status"`
	ActiveForm  string            `json:"active_form,omitempty"` // Present continuous form shown when in_progress
	Owner       string            `json:"owner,omitempty"`       // Agent ID if assigned
	Blocks      []string          `json:"blocks,omitempty"`      // Task IDs that cannot start until this completes
	BlockedBy   []string          `json:"blocked_by,omitempty"`  // Task IDs that must complete before this can start
	Metadata    map[string]any    `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// TaskCreate contains the fields for creating a new task.
type TaskCreate struct {
	Subject     string         `json:"subject"`
	Description string         `json:"description,omitempty"`
	Status      Status         `json:"status,omitempty"` // Defaults to pending
	ActiveForm  string         `json:"active_form,omitempty"`
	Blocks      []string       `json:"blocks,omitempty"`
	BlockedBy   []string       `json:"blocked_by,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TaskUpdate contains the fields for updating an existing task.
type TaskUpdate struct {
	Subject     *string        `json:"subject,omitempty"`
	Description *string        `json:"description,omitempty"`
	Status      *Status        `json:"status,omitempty"`
	ActiveForm  *string        `json:"active_form,omitempty"`
	Owner       *string        `json:"owner,omitempty"`
	AddBlocks   []string       `json:"add_blocks,omitempty"`
	AddBlockedBy []string      `json:"add_blocked_by,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"` // Keys with nil values are deleted
}

// TaskFilters specifies criteria for listing tasks.
type TaskFilters struct {
	Status   *Status `json:"status,omitempty"`
	Owner    *string `json:"owner,omitempty"`
	Blocked  *bool   `json:"blocked,omitempty"`   // If true, only blocked tasks; if false, only unblocked
}

// EventType represents the type of task event.
type EventType string

const (
	EventTypeCreated  EventType = "created"
	EventTypeUpdated  EventType = "updated"
	EventTypeDeleted  EventType = "deleted"
	EventTypeListSync EventType = "list_sync" // Full list resync (e.g., file changed)
)

// TaskEvent represents a change notification for tasks.
type TaskEvent struct {
	Type     EventType `json:"type"`
	ListID   string    `json:"list_id"`
	TaskID   string    `json:"task_id,omitempty"` // Empty for list-level events
	Task     *Task     `json:"task,omitempty"`    // Present for created/updated events
}

// Provider defines the interface for task management backends.
type Provider interface {
	// Name returns the provider identifier (e.g., "claude", "local").
	Name() string

	// ListTaskLists returns all task lists from this provider.
	ListTaskLists() ([]TaskList, error)

	// ListTasks returns tasks from a specific list, optionally filtered.
	ListTasks(listID string, filters TaskFilters) ([]Task, error)

	// GetTask retrieves a specific task by ID.
	GetTask(listID, taskID string) (*Task, error)

	// CreateTask creates a new task in the specified list.
	CreateTask(listID string, task *TaskCreate) (*Task, error)

	// UpdateTask modifies an existing task.
	UpdateTask(listID, taskID string, update *TaskUpdate) (*Task, error)

	// DeleteTask removes a task from the list.
	DeleteTask(listID, taskID string) error

	// Watch starts watching for task changes and returns a channel of events.
	// The channel is closed when the context is canceled.
	Watch(ctx context.Context) (<-chan TaskEvent, error)
}
