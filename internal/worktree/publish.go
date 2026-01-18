// Package worktree handles git worktree discovery and management.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

// Publisher handles publishing worktrees via PR or local merge.
type Publisher struct {
	config *config.Config
	store  *store.Store
}

// NewPublisher creates a new worktree publisher.
func NewPublisher(cfg *config.Config, st *store.Store) *Publisher {
	return &Publisher{config: cfg, store: st}
}

// PublishOptions configures the PR creation.
type PublishOptions struct {
	WorktreePath string
	Title        string // PR title (auto-generated if empty)
	Body         string // PR body (auto-generated if empty)
}

// PublishResult contains the result of publishing.
type PublishResult struct {
	PRURL  string `json:"pr_url"`
	Branch string `json:"branch"`
}

// PublishPR pushes the branch and creates a PR via gh CLI.
func (p *Publisher) PublishPR(opts PublishOptions) (*PublishResult, error) {
	// 1. Verify worktree exists in store
	wt, err := p.store.GetWorktree(opts.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}
	if wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", opts.WorktreePath)
	}
	if wt.IsMain {
		return nil, fmt.Errorf("cannot publish main worktree")
	}

	// 2. Verify clean working tree
	clean, err := isCleanWorkingTree(opts.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to check git status: %w", err)
	}
	if !clean {
		return nil, fmt.Errorf("working tree has uncommitted changes")
	}

	// 3. Get branch name
	branch := wt.Branch
	if branch == "" {
		branch = getCurrentBranch(opts.WorktreePath)
		if branch == "" {
			return nil, fmt.Errorf("failed to get branch")
		}
	}

	// 4. Push to remote
	logging.Info("pushing branch to remote", "branch", branch, "worktree", opts.WorktreePath)
	cmd := exec.Command("git", "push", "-u", "origin", "HEAD")
	cmd.Dir = opts.WorktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git push failed: %w\n%s", err, string(output))
	}

	// 5. Generate PR title and body
	title := opts.Title
	if title == "" {
		title = generatePRTitle(wt)
	}

	body := opts.Body
	if body == "" {
		body = generatePRBody(wt)
	}

	// 6. Create PR via gh CLI
	logging.Info("creating PR", "title", title, "worktree", opts.WorktreePath)
	prURL, err := createPR(opts.WorktreePath, title, body)
	if err != nil {
		return nil, fmt.Errorf("gh pr create failed: %w", err)
	}

	// 7. Update store
	if err := p.store.UpdateWorktreePRURL(opts.WorktreePath, prURL); err != nil {
		logging.Warn("failed to store PR URL", "error", err)
	}
	if err := p.store.UpdateWorktreeStatus(opts.WorktreePath, store.WorktreeStatusPublished); err != nil {
		logging.Warn("failed to update worktree status", "error", err)
	}

	logging.Info("PR created successfully", "url", prURL, "branch", branch)

	return &PublishResult{
		PRURL:  prURL,
		Branch: branch,
	}, nil
}

// MergeLocal merges the branch into main locally.
func (p *Publisher) MergeLocal(worktreePath string) error {
	// 1. Verify worktree exists
	wt, err := p.store.GetWorktree(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	if wt == nil {
		return fmt.Errorf("worktree not found: %s", worktreePath)
	}
	if wt.IsMain {
		return fmt.Errorf("cannot merge main worktree")
	}

	// 2. Verify clean working tree
	clean, err := isCleanWorkingTree(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree has uncommitted changes")
	}

	// 3. Get branch name and main repo path
	branch := wt.Branch
	if branch == "" {
		branch = getCurrentBranch(worktreePath)
		if branch == "" {
			return fmt.Errorf("failed to get branch")
		}
	}

	mainRepoPath, err := getMainRepoPath(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to get main repo path: %w", err)
	}

	// 4. Get default branch name
	defaultBranch := getDefaultBranch(mainRepoPath)
	if defaultBranch == "" {
		defaultBranch = "main" // Fallback
	}

	// 5. Checkout default branch in main repo
	logging.Info("checking out default branch", "branch", defaultBranch, "repo", mainRepoPath)
	cmd := exec.Command("git", "checkout", defaultBranch)
	cmd.Dir = mainRepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout %s failed: %w\n%s", defaultBranch, err, string(output))
	}

	// 6. Merge the branch
	logging.Info("merging branch", "branch", branch, "into", defaultBranch)
	cmd = exec.Command("git", "merge", "--no-ff", branch, "-m", fmt.Sprintf("Merge branch '%s'", branch))
	cmd.Dir = mainRepoPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge failed: %w\n%s", err, string(output))
	}

	// 7. Update status
	if err := p.store.UpdateWorktreeStatus(worktreePath, store.WorktreeStatusMerged); err != nil {
		logging.Warn("failed to update worktree status", "error", err)
	}

	logging.Info("branch merged successfully", "branch", branch)

	return nil
}

// Cleanup removes a worktree and optionally deletes the branch.
func (p *Publisher) Cleanup(worktreePath string, deleteBranch bool) error {
	// 1. Verify worktree exists
	wt, err := p.store.GetWorktree(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	if wt == nil {
		return fmt.Errorf("worktree not found: %s", worktreePath)
	}
	if wt.IsMain {
		return fmt.Errorf("cannot cleanup main worktree")
	}

	// 2. Get branch name and main repo path before removal
	branch := wt.Branch
	if branch == "" {
		branch = getCurrentBranch(worktreePath)
	}

	mainRepoPath, err := getMainRepoPath(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to get main repo path: %w", err)
	}

	// 3. Remove worktree via git
	logging.Info("removing worktree", "path", worktreePath)
	cmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
	cmd.Dir = mainRepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try to remove the directory directly if git worktree remove fails
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			return fmt.Errorf("git worktree remove failed: %w\n%s", err, string(output))
		}
		logging.Warn("forced directory removal after git worktree remove failed", "path", worktreePath)
	}

	// 4. Delete branch if requested
	if deleteBranch && branch != "" {
		logging.Info("deleting branch", "branch", branch)
		cmd = exec.Command("git", "branch", "-d", branch)
		cmd.Dir = mainRepoPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			// Try force delete
			cmd = exec.Command("git", "branch", "-D", branch)
			cmd.Dir = mainRepoPath
			if forceOutput, forceErr := cmd.CombinedOutput(); forceErr != nil {
				logging.Warn("failed to delete branch", "branch", branch, "error", string(forceOutput))
			}
		}
	}

	// 5. Remove from store
	if err := p.store.DeleteWorktree(worktreePath); err != nil {
		logging.Warn("failed to delete worktree from store", "error", err)
	}

	logging.Info("worktree cleaned up successfully", "path", worktreePath)

	return nil
}

// Helper functions

func isCleanWorkingTree(path string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) == 0, nil
}

func getMainRepoPath(worktreePath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = worktreePath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(string(output))
	// The git common dir ends in .git, so the repo is its parent
	if strings.HasSuffix(gitDir, "/.git") || strings.HasSuffix(gitDir, "\\.git") {
		return filepath.Dir(gitDir), nil
	}
	// For bare repos or other cases
	return gitDir, nil
}

func generatePRTitle(wt *store.Worktree) string {
	// Format: {TICKET-ID}: {description}
	if wt.TicketID != nil && *wt.TicketID != "" {
		desc := "Feature implementation"
		if wt.Description != nil && *wt.Description != "" {
			desc = *wt.Description
		}
		return fmt.Sprintf("%s: %s", *wt.TicketID, desc)
	}

	// Fallback to branch name
	if wt.Branch != "" {
		return wt.Branch
	}

	return "Feature implementation"
}

func generatePRBody(wt *store.Worktree) string {
	var parts []string

	parts = append(parts, "## Summary")
	if wt.Description != nil && *wt.Description != "" {
		parts = append(parts, *wt.Description)
	} else {
		parts = append(parts, "Implementation via Athena workflow.")
	}

	parts = append(parts, "")
	parts = append(parts, "## Test Plan")
	parts = append(parts, "- [ ] Manual testing")
	parts = append(parts, "- [ ] Unit tests pass")

	if wt.TicketID != nil && *wt.TicketID != "" {
		parts = append(parts, "")
		parts = append(parts, fmt.Sprintf("Fixes %s", *wt.TicketID))
	}

	parts = append(parts, "")
	parts = append(parts, "---")
	parts = append(parts, "Generated with [Athena](https://github.com/drewfead/athena)")

	return strings.Join(parts, "\n")
}

func createPR(worktreePath, title, body string) (string, error) {
	cmd := exec.Command("gh", "pr", "create", "--title", title, "--body", body)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, string(output))
	}

	// gh pr create outputs the URL
	prURL := strings.TrimSpace(string(output))
	return prURL, nil
}
