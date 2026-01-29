// Package pm provides Project Management plugins.
package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Linear implements the PM Provider interface.
// Uses the Linear CLI or MCP server for operations.
type Linear struct {
	*BasePM
}

// NewLinear creates a new Linear PM plugin.
func NewLinear() *Linear {
	return &Linear{
		BasePM: NewBasePM("linear"),
	}
}

func (l *Linear) GetIssue(ctx context.Context, issueKey string) (*Issue, error) {
	// Try using the linear CLI if available
	// Otherwise, this would call the MCP server
	cmd := exec.CommandContext(ctx, "linear", "issue", "view", issueKey, "--json")
	out, err := cmd.Output()
	if err != nil {
		// Fall back to describing the issue via the API pattern
		return nil, fmt.Errorf("linear issue view failed: %w (ensure linear CLI or MCP is configured)", err)
	}

	var result struct {
		ID          string `json:"id"`
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       struct {
			Name string `json:"name"`
		} `json:"state"`
		Priority int `json:"priority"`
		Assignee struct {
			Name string `json:"name"`
		} `json:"assignee"`
		URL       string   `json:"url"`
		Labels    []string `json:"labelIds"`
		CreatedAt string   `json:"createdAt"`
		UpdatedAt string   `json:"updatedAt"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	return &Issue{
		ID:          result.ID,
		Key:         result.Identifier,
		Title:       result.Title,
		Description: result.Description,
		State:       linearStateToIssueState(result.State.Name),
		Priority:    Priority(result.Priority),
		Assignee:    result.Assignee.Name,
		URL:         result.URL,
		Labels:      result.Labels,
		CreatedAt:   result.CreatedAt,
		UpdatedAt:   result.UpdatedAt,
	}, nil
}

func (l *Linear) ListIssues(ctx context.Context, project string, state IssueState) ([]*Issue, error) {
	args := []string{"issue", "list", "--team", project, "--json"}
	if state != "" {
		args = append(args, "--state", string(state))
	}

	cmd := exec.CommandContext(ctx, "linear", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("linear issue list failed: %w", err)
	}

	var results []struct {
		ID          string `json:"id"`
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       struct {
			Name string `json:"name"`
		} `json:"state"`
		Priority int `json:"priority"`
		Assignee struct {
			Name string `json:"name"`
		} `json:"assignee"`
		URL       string   `json:"url"`
		Labels    []string `json:"labelIds"`
		CreatedAt string   `json:"createdAt"`
		UpdatedAt string   `json:"updatedAt"`
	}

	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}

	issues := make([]*Issue, len(results))
	for i, r := range results {
		issues[i] = &Issue{
			ID:          r.ID,
			Key:         r.Identifier,
			Title:       r.Title,
			Description: r.Description,
			State:       linearStateToIssueState(r.State.Name),
			Priority:    Priority(r.Priority),
			Assignee:    r.Assignee.Name,
			URL:         r.URL,
			Labels:      r.Labels,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		}
	}

	return issues, nil
}

func (l *Linear) CreateIssue(ctx context.Context, issue *Issue) (*Issue, error) {
	args := []string{"issue", "create",
		"--title", issue.Title,
		"--json",
	}
	if issue.Description != "" {
		args = append(args, "--description", issue.Description)
	}

	cmd := exec.CommandContext(ctx, "linear", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("linear issue create failed: %w", err)
	}

	var result struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
		URL        string `json:"url"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	issue.ID = result.ID
	issue.Key = result.Identifier
	issue.URL = result.URL
	return issue, nil
}

func (l *Linear) UpdateIssueState(ctx context.Context, issueKey string, state IssueState) error {
	stateName := issueStateToLinearState(state)
	cmd := exec.CommandContext(ctx, "linear", "issue", "update", issueKey, "--state", stateName)
	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("linear issue update failed: %w", err)
	}
	return nil
}

func (l *Linear) LinkPR(ctx context.Context, issueKey, prURL string) error {
	// Linear auto-links PRs when the branch name contains the issue key
	// or when the PR description contains the issue key
	// This is a no-op for Linear
	return nil
}

func linearStateToIssueState(state string) IssueState {
	switch strings.ToLower(state) {
	case "backlog":
		return IssueStateBacklog
	case "todo", "unstarted":
		return IssueStateTodo
	case "in progress", "started":
		return IssueStateInProgress
	case "done", "completed":
		return IssueStateDone
	case "canceled", "cancelled":
		return IssueStateCanceled
	default:
		return IssueStateTodo
	}
}

func issueStateToLinearState(state IssueState) string {
	switch state {
	case IssueStateBacklog:
		return "Backlog"
	case IssueStateTodo:
		return "Todo"
	case IssueStateInProgress:
		return "In Progress"
	case IssueStateDone:
		return "Done"
	case IssueStateCanceled:
		return "Canceled"
	default:
		return "Todo"
	}
}
