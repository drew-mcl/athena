package vcs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GitLab implements the VCS Provider interface using the glab CLI.
type GitLab struct {
	*BaseVCS
}

// NewGitLab creates a new GitLab VCS plugin.
func NewGitLab() *GitLab {
	return &GitLab{
		BaseVCS: NewBaseVCS("gitlab"),
	}
}

func (g *GitLab) GetPR(ctx context.Context, repo, branch string) (*PullRequest, error) {
	cmd := exec.CommandContext(ctx, "glab", "mr", "view", branch, "--repo", repo, "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("glab mr view failed: %w", err)
	}

	var result struct {
		IID            int    `json:"iid"`
		Title          string `json:"title"`
		State          string `json:"state"`
		SourceBranch   string `json:"source_branch"`
		TargetBranch   string `json:"target_branch"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		WebURL         string `json:"web_url"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	return &PullRequest{
		ID:          strconv.Itoa(result.IID),
		Number:      result.IID,
		Title:       result.Title,
		State:       glabStateToPRState(result.State),
		Branch:      result.SourceBranch,
		BaseBranch:  result.TargetBranch,
		MergeCommit: result.MergeCommitSHA,
		URL:         result.WebURL,
	}, nil
}

func (g *GitLab) ListOpenPRs(ctx context.Context, repo string) ([]*PullRequest, error) {
	cmd := exec.CommandContext(ctx, "glab", "mr", "list", "--repo", repo, "--state", "opened", "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("glab mr list failed: %w", err)
	}

	var results []struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		State        string `json:"state"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		WebURL       string `json:"web_url"`
	}

	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}

	prs := make([]*PullRequest, len(results))
	for i, r := range results {
		prs[i] = &PullRequest{
			ID:         strconv.Itoa(r.IID),
			Number:     r.IID,
			Title:      r.Title,
			State:      glabStateToPRState(r.State),
			Branch:     r.SourceBranch,
			BaseBranch: r.TargetBranch,
			URL:        r.WebURL,
		}
	}

	return prs, nil
}

func (g *GitLab) GetPRState(ctx context.Context, repo string, prNumber int) (PRState, error) {
	cmd := exec.CommandContext(ctx, "glab", "mr", "view", strconv.Itoa(prNumber),
		"--repo", repo, "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("glab mr view failed: %w", err)
	}

	var result struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}

	return glabStateToPRState(result.State), nil
}

func (g *GitLab) GetMergeCommit(ctx context.Context, repo string, prNumber int) (string, error) {
	cmd := exec.CommandContext(ctx, "glab", "mr", "view", strconv.Itoa(prNumber),
		"--repo", repo, "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("glab mr view failed: %w", err)
	}

	var result struct {
		MergeCommitSHA string `json:"merge_commit_sha"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}

	return result.MergeCommitSHA, nil
}

func (g *GitLab) GetCIStatus(ctx context.Context, repo, branch string) (*CIRun, error) {
	cmd := exec.CommandContext(ctx, "glab", "ci", "view", "--repo", repo, "--branch", branch, "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("glab ci view failed: %w", err)
	}

	var result struct {
		ID        int    `json:"id"`
		Status    string `json:"status"`
		Ref       string `json:"ref"`
		SHA       string `json:"sha"`
		WebURL    string `json:"web_url"`
		CreatedAt string `json:"created_at"`
		Duration  int    `json:"duration"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	return &CIRun{
		ID:        strconv.Itoa(result.ID),
		Status:    glabCIStatusToCIStatus(result.Status),
		Branch:    result.Ref,
		Commit:    result.SHA,
		URL:       result.WebURL,
		StartedAt: result.CreatedAt,
		Duration:  result.Duration,
	}, nil
}

func (g *GitLab) ListCIRuns(ctx context.Context, repo, branch string, limit int) ([]*CIRun, error) {
	cmd := exec.CommandContext(ctx, "glab", "ci", "list", "--repo", repo, "--branch", branch,
		"--per-page", strconv.Itoa(limit), "--output", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("glab ci list failed: %w", err)
	}

	var results []struct {
		ID        int    `json:"id"`
		Status    string `json:"status"`
		Ref       string `json:"ref"`
		SHA       string `json:"sha"`
		WebURL    string `json:"web_url"`
		CreatedAt string `json:"created_at"`
		Duration  int    `json:"duration"`
	}

	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}

	runs := make([]*CIRun, len(results))
	for i, r := range results {
		runs[i] = &CIRun{
			ID:        strconv.Itoa(r.ID),
			Status:    glabCIStatusToCIStatus(r.Status),
			Branch:    r.Ref,
			Commit:    r.SHA,
			URL:       r.WebURL,
			StartedAt: r.CreatedAt,
			Duration:  r.Duration,
		}
	}

	return runs, nil
}

func glabStateToPRState(state string) PRState {
	switch strings.ToLower(state) {
	case "opened":
		return PRStateOpen
	case "merged":
		return PRStateMerged
	case "closed":
		return PRStateClosed
	default:
		return PRStateOpen
	}
}

func glabCIStatusToCIStatus(status string) CIStatus {
	switch strings.ToLower(status) {
	case "pending", "created":
		return CIStatusPending
	case "running":
		return CIStatusRunning
	case "success":
		return CIStatusPassed
	case "failed", "canceled", "skipped":
		return CIStatusFailed
	default:
		return CIStatusPending
	}
}
