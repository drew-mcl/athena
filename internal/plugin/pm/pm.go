// Package pm provides Project Management plugin interfaces.
package pm

import (
	"context"

	"github.com/drewfead/athena/internal/plugin"
)

// IssueState represents the state of an issue.
type IssueState string

const (
	IssueStateBacklog    IssueState = "backlog"
	IssueStateTodo       IssueState = "todo"
	IssueStateInProgress IssueState = "in_progress"
	IssueStateDone       IssueState = "done"
	IssueStateCanceled   IssueState = "canceled"
)

// Priority represents issue priority.
type Priority int

const (
	PriorityNone   Priority = 0
	PriorityUrgent Priority = 1
	PriorityHigh   Priority = 2
	PriorityMedium Priority = 3
	PriorityLow    Priority = 4
)

// Issue represents a project management issue/ticket.
type Issue struct {
	ID          string
	Key         string     // e.g., "ENG-123"
	Title       string
	Description string
	State       IssueState
	Priority    Priority
	Assignee    string
	URL         string
	Labels      []string
	CreatedAt   string
	UpdatedAt   string
}

// Provider defines the PM provider interface.
type Provider interface {
	plugin.Plugin

	// Issue Operations
	GetIssue(ctx context.Context, issueKey string) (*Issue, error)
	ListIssues(ctx context.Context, project string, state IssueState) ([]*Issue, error)
	CreateIssue(ctx context.Context, issue *Issue) (*Issue, error)
	UpdateIssueState(ctx context.Context, issueKey string, state IssueState) error

	// Linking
	LinkPR(ctx context.Context, issueKey, prURL string) error
}

// BasePM provides common PM plugin functionality.
type BasePM struct {
	*plugin.BasePlugin
}

// NewBasePM creates a base PM plugin.
func NewBasePM(name string) *BasePM {
	return &BasePM{
		BasePlugin: plugin.NewBasePlugin(name, plugin.CategoryPM),
	}
}
