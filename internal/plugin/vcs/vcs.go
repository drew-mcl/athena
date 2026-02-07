// Package vcs provides Version Control System plugin interfaces.
package vcs

import (
	"context"

	"github.com/drewfead/athena/internal/plugin"
)

// PRState represents the state of a pull/merge request.
type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateMerged PRState = "merged"
	PRStateClosed PRState = "closed"
)

// PullRequest represents a pull/merge request.
type PullRequest struct {
	ID          string
	Number      int
	Title       string
	State       PRState
	Branch      string // Source branch
	BaseBranch  string // Target branch (usually main)
	MergeCommit string // Set when merged
	URL         string
}

// CIStatus represents CI/CD pipeline status.
type CIStatus string

const (
	CIStatusPending CIStatus = "pending"
	CIStatusRunning CIStatus = "running"
	CIStatusPassed  CIStatus = "passed"
	CIStatusFailed  CIStatus = "failed"
)

// CIRun represents a CI/CD pipeline run.
type CIRun struct {
	ID        string
	Status    CIStatus
	Branch    string
	Commit    string
	URL       string
	StartedAt string
	Duration  int // seconds
}

// Provider defines the VCS provider interface.
type Provider interface {
	plugin.Plugin

	// PR Operations
	GetPR(ctx context.Context, repo, branch string) (*PullRequest, error)
	ListOpenPRs(ctx context.Context, repo string) ([]*PullRequest, error)
	GetPRState(ctx context.Context, repo string, prNumber int) (PRState, error)
	GetMergeCommit(ctx context.Context, repo string, prNumber int) (string, error)

	// CI Operations (optional - some providers may not support)
	GetCIStatus(ctx context.Context, repo, branch string) (*CIRun, error)
	ListCIRuns(ctx context.Context, repo, branch string, limit int) ([]*CIRun, error)
}

// BaseVCS provides common VCS plugin functionality.
type BaseVCS struct {
	*plugin.BasePlugin
}

// NewBaseVCS creates a base VCS plugin.
func NewBaseVCS(name string) *BaseVCS {
	return &BaseVCS{
		BasePlugin: plugin.NewBasePlugin(name, plugin.CategoryVCS),
	}
}
