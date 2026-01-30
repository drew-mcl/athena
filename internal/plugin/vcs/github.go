package vcs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GitHub implements the VCS Provider interface using the gh CLI.
type GitHub struct {
	*BaseVCS
}

// NewGitHub creates a new GitHub VCS plugin.
func NewGitHub() *GitHub {
	return &GitHub{
		BaseVCS: NewBaseVCS("github"),
	}
}

func (g *GitHub) GetPR(ctx context.Context, repo, branch string) (*PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", branch, "--repo", repo, "--json",
		"number,title,state,headRefName,baseRefName,mergeCommit,url")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view failed: %w", err)
	}

	var result struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		State       string `json:"state"`
		HeadRefName string `json:"headRefName"`
		BaseRefName string `json:"baseRefName"`
		MergeCommit struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	pr := &PullRequest{
		ID:         strconv.Itoa(result.Number),
		Number:     result.Number,
		Title:      result.Title,
		State:      ghStateToPRState(result.State),
		Branch:     result.HeadRefName,
		BaseBranch: result.BaseRefName,
		URL:        result.URL,
	}

	if result.MergeCommit.OID != "" {
		pr.MergeCommit = result.MergeCommit.OID
	}

	return pr, nil
}

func (g *GitHub) ListOpenPRs(ctx context.Context, repo string) ([]*PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--repo", repo, "--state", "open", "--json",
		"number,title,state,headRefName,baseRefName,url")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr list failed: %w", err)
	}

	var results []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		State       string `json:"state"`
		HeadRefName string `json:"headRefName"`
		BaseRefName string `json:"baseRefName"`
		URL         string `json:"url"`
	}

	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}

	prs := make([]*PullRequest, len(results))
	for i, r := range results {
		prs[i] = &PullRequest{
			ID:         strconv.Itoa(r.Number),
			Number:     r.Number,
			Title:      r.Title,
			State:      ghStateToPRState(r.State),
			Branch:     r.HeadRefName,
			BaseBranch: r.BaseRefName,
			URL:        r.URL,
		}
	}

	return prs, nil
}

func (g *GitHub) GetPRState(ctx context.Context, repo string, prNumber int) (PRState, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(prNumber),
		"--repo", repo, "--json", "state")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view failed: %w", err)
	}

	var result struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}

	return ghStateToPRState(result.State), nil
}

func (g *GitHub) GetMergeCommit(ctx context.Context, repo string, prNumber int) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(prNumber),
		"--repo", repo, "--json", "mergeCommit")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view failed: %w", err)
	}

	var result struct {
		MergeCommit struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}

	return result.MergeCommit.OID, nil
}

func (g *GitHub) GetCIStatus(ctx context.Context, repo, branch string) (*CIRun, error) {
	cmd := exec.CommandContext(ctx, "gh", "run", "list", "--repo", repo, "--branch", branch,
		"--limit", "1", "--json", "databaseId,status,conclusion,headBranch,headSha,url,startedAt")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh run list failed: %w", err)
	}

	var results []struct {
		DatabaseID int    `json:"databaseId"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadBranch string `json:"headBranch"`
		HeadSha    string `json:"headSha"`
		URL        string `json:"url"`
		StartedAt  string `json:"startedAt"`
	}

	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	r := results[0]
	return &CIRun{
		ID:        strconv.Itoa(r.DatabaseID),
		Status:    ghCIStatusToCIStatus(r.Status, r.Conclusion),
		Branch:    r.HeadBranch,
		Commit:    r.HeadSha,
		URL:       r.URL,
		StartedAt: r.StartedAt,
	}, nil
}

func (g *GitHub) ListCIRuns(ctx context.Context, repo, branch string, limit int) ([]*CIRun, error) {
	cmd := exec.CommandContext(ctx, "gh", "run", "list", "--repo", repo, "--branch", branch,
		"--limit", strconv.Itoa(limit), "--json", "databaseId,status,conclusion,headBranch,headSha,url,startedAt")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh run list failed: %w", err)
	}

	var results []struct {
		DatabaseID int    `json:"databaseId"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadBranch string `json:"headBranch"`
		HeadSha    string `json:"headSha"`
		URL        string `json:"url"`
		StartedAt  string `json:"startedAt"`
	}

	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}

	runs := make([]*CIRun, len(results))
	for i, r := range results {
		runs[i] = &CIRun{
			ID:        strconv.Itoa(r.DatabaseID),
			Status:    ghCIStatusToCIStatus(r.Status, r.Conclusion),
			Branch:    r.HeadBranch,
			Commit:    r.HeadSha,
			URL:       r.URL,
			StartedAt: r.StartedAt,
		}
	}

	return runs, nil
}

func ghStateToPRState(state string) PRState {
	switch strings.ToLower(state) {
	case "open":
		return PRStateOpen
	case "merged":
		return PRStateMerged
	case "closed":
		return PRStateClosed
	default:
		return PRStateOpen
	}
}

func ghCIStatusToCIStatus(status, conclusion string) CIStatus {
	switch strings.ToLower(status) {
	case "queued", "pending":
		return CIStatusPending
	case "in_progress":
		return CIStatusRunning
	case "completed":
		switch strings.ToLower(conclusion) {
		case "success":
			return CIStatusPassed
		default:
			return CIStatusFailed
		}
	default:
		return CIStatusPending
	}
}
