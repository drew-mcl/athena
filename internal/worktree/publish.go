// Package worktree handles git worktree discovery and management.
package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/executil"
	"github.com/drewfead/athena/internal/github"
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
	Archetype    string // Agent archetype (used for identity resolution)
}

// PublishResult contains the result of publishing.
type PublishResult struct {
	PRURL  string `json:"pr_url"`
	Branch string `json:"branch"`
}

// MergeResult contains the result of a local merge attempt.
type MergeResult struct {
	Success      bool   `json:"success"`
	HasConflicts bool   `json:"has_conflicts"`
	Branch       string `json:"branch"`
	Message      string `json:"message,omitempty"`
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
	cmd, err := executil.Command("git", "push", "-u", "origin", "HEAD")
	if err != nil {
		return nil, err
	}
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

	// 6. Create PR - try GitHub App first, fall back to gh CLI
	logging.Info("creating PR", "title", title, "worktree", opts.WorktreePath)

	var prURL string
	appClient := github.IdentityForArchetype(p.config, opts.Archetype)
	if appClient != nil {
		// Use GitHub App for PR creation (shows as bot)
		prURL, err = p.createPRWithApp(appClient, opts.WorktreePath, branch, title, body)
		if err != nil {
			logging.Warn("GitHub App PR creation failed, falling back to gh CLI",
				"error", err, "identity", appClient.String())
			// Fall back to gh CLI
			prURL, err = createPR(opts.WorktreePath, title, body)
			if err != nil {
				return nil, fmt.Errorf("PR creation failed: %w", err)
			}
		}
	} else {
		// No GitHub App configured, use gh CLI
		prURL, err = createPR(opts.WorktreePath, title, body)
		if err != nil {
			return nil, fmt.Errorf("gh pr create failed: %w", err)
		}
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
// Returns a result indicating success or conflict (for auto-resolution).
func (p *Publisher) MergeLocal(worktreePath string) (*MergeResult, error) {
	// 1. Verify worktree exists
	wt, err := p.store.GetWorktree(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}
	if wt == nil {
		return nil, fmt.Errorf("worktree not found: %s", worktreePath)
	}
	if wt.IsMain {
		return nil, fmt.Errorf("cannot merge main worktree")
	}

	// 2. Verify clean working tree
	clean, err := isCleanWorkingTree(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to check git status: %w", err)
	}
	if !clean {
		return nil, fmt.Errorf("working tree has uncommitted changes")
	}

	// 3. Get branch name and main repo path
	branch := wt.Branch
	if branch == "" {
		branch = getCurrentBranch(worktreePath)
		if branch == "" {
			return nil, fmt.Errorf("failed to get branch")
		}
	}

	mainRepoPath, err := getMainRepoPath(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get main repo path: %w", err)
	}

	// 4. Get default branch name
	defaultBranch := getDefaultBranch(mainRepoPath)
	if defaultBranch == "" {
		defaultBranch = "main" // Fallback
	}

	// 5. Checkout default branch in main repo
	logging.Info("checking out default branch", "branch", defaultBranch, "repo", mainRepoPath)
	cmd, err := executil.Command("git", "checkout", defaultBranch)
	if err != nil {
		return nil, err
	}
	cmd.Dir = mainRepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git checkout %s failed: %w\n%s", defaultBranch, err, string(output))
	}

	// 6. Merge the branch
	logging.Info("merging branch", "branch", branch, "into", defaultBranch)
	cmd, err = executil.Command("git", "merge", "--no-ff", branch, "-m", fmt.Sprintf("Merge branch '%s'", branch))
	if err != nil {
		return nil, err
	}
	cmd.Dir = mainRepoPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		// Check if this is a merge conflict
		if isMergeConflict(string(output)) {
			logging.Info("merge conflict detected, aborting merge in main", "branch", branch)
			// Abort the merge in main repo to keep it clean
			if abortCmd, err := executil.Command("git", "merge", "--abort"); err == nil {
				abortCmd.Dir = mainRepoPath
				abortCmd.CombinedOutput() // Best effort
			}

			return &MergeResult{
				Success:      false,
				HasConflicts: true,
				Branch:       branch,
				Message:      fmt.Sprintf("Merge conflict detected between %s and %s", branch, defaultBranch),
			}, nil
		}
		return nil, fmt.Errorf("git merge failed: %w\n%s", err, string(output))
	}

	// 7. Update status
	if err := p.store.UpdateWorktreeStatus(worktreePath, store.WorktreeStatusMerged); err != nil {
		logging.Warn("failed to update worktree status", "error", err)
	}

	logging.Info("branch merged successfully", "branch", branch)

	return &MergeResult{
		Success: true,
		Branch:  branch,
		Message: fmt.Sprintf("Successfully merged %s into %s", branch, defaultBranch),
	}, nil
}

// isMergeConflict checks if git output indicates a merge conflict.
func isMergeConflict(output string) bool {
	conflictIndicators := []string{
		"CONFLICT",
		"Automatic merge failed",
		"fix conflicts and then commit",
	}
	for _, indicator := range conflictIndicators {
		if strings.Contains(output, indicator) {
			return true
		}
	}
	return false
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
	cmd, err := executil.Command("git", "worktree", "remove", worktreePath, "--force")
	if err != nil {
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			return err
		}
		logging.Warn("forced directory removal after git worktree remove failed", "path", worktreePath)
	} else {
		cmd.Dir = mainRepoPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Try to remove the directory directly if git worktree remove fails
			if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
				return fmt.Errorf("git worktree remove failed: %w\n%s", err, string(output))
			}
			logging.Warn("forced directory removal after git worktree remove failed", "path", worktreePath)
		}
	}

	// 4. Delete branch if requested
	if deleteBranch && branch != "" {
		logging.Info("deleting branch", "branch", branch)
		cmd, err = executil.Command("git", "branch", "-d", branch)
		if err == nil {
			cmd.Dir = mainRepoPath
			output, err = cmd.CombinedOutput()
		}
		if err != nil {
			// Try force delete
			cmd, err = executil.Command("git", "branch", "-D", branch)
			if err == nil {
				cmd.Dir = mainRepoPath
				if forceOutput, forceErr := cmd.CombinedOutput(); forceErr != nil {
					logging.Warn("failed to delete branch", "branch", branch, "error", string(forceOutput))
				}
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
	cmd, err := executil.Command("git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) == 0, nil
}

func getMainRepoPath(worktreePath string) (string, error) {
	cmd, err := executil.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
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
	cmd, err := executil.Command("gh", "pr", "create", "--title", title, "--body", body)
	if err != nil {
		return "", err
	}
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, string(output))
	}

	// gh pr create outputs the URL
	prURL := strings.TrimSpace(string(output))
	return prURL, nil
}

// createPRWithApp creates a PR using a GitHub App client.
// This makes the PR appear as created by the bot identity.
func (p *Publisher) createPRWithApp(client *github.AppClient, worktreePath, branch, title, body string) (string, error) {
	// Get the remote URL to extract owner/repo
	remoteURL, err := getRemoteURL(worktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL: %w", err)
	}

	owner, repo, err := github.ParseRepoFromRemote(remoteURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse remote URL: %w", err)
	}

	// Get the default branch for base
	mainRepoPath, err := getMainRepoPath(worktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to get main repo path: %w", err)
	}
	baseBranch := getDefaultBranch(mainRepoPath)
	if baseBranch == "" {
		baseBranch = "main"
	}

	result, err := client.CreatePR(github.PROptions{
		Owner: owner,
		Repo:  repo,
		Title: title,
		Body:  body,
		Head:  branch,
		Base:  baseBranch,
	})
	if err != nil {
		return "", err
	}

	logging.Info("PR created via GitHub App",
		"url", result.HTMLURL,
		"number", result.Number,
		"identity", client.String())

	return result.HTMLURL, nil
}

// getRemoteURL returns the origin remote URL for a git repository.
func getRemoteURL(path string) (string, error) {
	cmd, err := executil.Command("git", "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
