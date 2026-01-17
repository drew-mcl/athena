package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/store"
)

// Provisioner creates and manages git worktrees for jobs.
type Provisioner struct {
	config *config.Config
	store  *store.Store
}

// NewProvisioner creates a new worktree provisioner.
func NewProvisioner(cfg *config.Config, st *store.Store) *Provisioner {
	return &Provisioner{config: cfg, store: st}
}

// CreateForJob creates a new worktree for a job.
func (p *Provisioner) CreateForJob(project string, job *store.Job) (*store.Worktree, error) {
	// Find the main repo for this project
	mainRepo, err := p.findMainRepo(project)
	if err != nil {
		return nil, fmt.Errorf("could not find main repo for project %s: %w", project, err)
	}

	// Generate worktree name and branch
	wtName := p.generateWorktreeName(project, job)
	branchName := p.generateBranchName(job)
	worktreePath := filepath.Join(filepath.Dir(mainRepo), wtName)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	// Create the worktree with a new branch
	cmd := exec.Command("git", "worktree", "add", "-b", branchName, worktreePath)
	cmd.Dir = mainRepo
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %s: %w", string(output), err)
	}

	// Register in store
	wt := &store.Worktree{
		Path:    worktreePath,
		Project: project,
		Branch:  branchName,
		IsMain:  false,
		JobID:   &job.ID,
	}
	if err := p.store.UpsertWorktree(wt); err != nil {
		return nil, fmt.Errorf("failed to register worktree: %w", err)
	}

	return wt, nil
}

// Remove removes a worktree and optionally its branch.
func (p *Provisioner) Remove(worktreePath string, deleteBranch bool) error {
	wt, err := p.store.GetWorktree(worktreePath)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("worktree not found: %s", worktreePath)
	}
	if wt.IsMain {
		return fmt.Errorf("cannot remove main repository")
	}

	// Find the main repo
	mainRepo, err := p.findMainRepo(wt.Project)
	if err != nil {
		return err
	}

	// Remove the worktree
	cmd := exec.Command("git", "worktree", "remove", worktreePath)
	cmd.Dir = mainRepo
	if _, err := cmd.CombinedOutput(); err != nil {
		// Try force removal
		cmd = exec.Command("git", "worktree", "remove", "--force", worktreePath)
		cmd.Dir = mainRepo
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to remove worktree: %s: %w", string(output), err)
		}
	}

	// Optionally delete the branch
	if deleteBranch && wt.Branch != "" {
		cmd = exec.Command("git", "branch", "-d", wt.Branch)
		cmd.Dir = mainRepo
		cmd.Run() // Ignore errors - branch might not exist or might have unmerged changes
	}

	// Remove from store
	return p.store.DeleteWorktree(worktreePath)
}

// Prune removes worktrees for branches that have been merged.
func (p *Provisioner) Prune(project string) ([]string, error) {
	worktrees, err := p.store.ListWorktrees(project)
	if err != nil {
		return nil, err
	}

	mainRepo, err := p.findMainRepo(project)
	if err != nil {
		return nil, err
	}

	var pruned []string
	for _, wt := range worktrees {
		if wt.IsMain {
			continue
		}

		// Check if branch is merged
		if !p.isBranchMerged(mainRepo, wt.Branch) {
			continue
		}

		// Check if an agent is still running
		if wt.AgentID != nil {
			agent, _ := p.store.GetAgent(*wt.AgentID)
			if agent != nil && isAgentActive(agent.Status) {
				continue // Don't prune active work
			}
		}

		if err := p.Remove(wt.Path, true); err != nil {
			continue // Log but continue
		}
		pruned = append(pruned, wt.Path)
	}

	return pruned, nil
}

// PruneStale runs git worktree prune to clean up stale entries.
func (p *Provisioner) PruneStale(project string) error {
	mainRepo, err := p.findMainRepo(project)
	if err != nil {
		return err
	}

	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = mainRepo
	return cmd.Run()
}

// GetStatus returns the git status for a worktree.
func (p *Provisioner) GetStatus(worktreePath string) (*WorktreeStatus, error) {
	status := &WorktreeStatus{Path: worktreePath}

	// Get branch
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = worktreePath
	if output, err := cmd.Output(); err == nil {
		status.Branch = strings.TrimSpace(string(output))
	}

	// Get status
	cmd = exec.Command("git", "status", "--porcelain")
	cmd.Dir = worktreePath
	output, err := cmd.Output()
	if err != nil {
		return status, nil
	}

	for _, line := range strings.Split(string(output), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'M', 'A', 'D', 'R', 'C':
			status.Staged++
		}
		switch line[1] {
		case 'M', 'A', 'D', 'R', 'C', '?':
			status.Modified++
		}
	}

	status.Clean = status.Staged == 0 && status.Modified == 0
	return status, nil
}

// WorktreeStatus represents the git status of a worktree.
type WorktreeStatus struct {
	Path     string
	Branch   string
	Clean    bool
	Staged   int
	Modified int
	Merged   bool
	Stale    bool
	Gone     bool
}

func (p *Provisioner) findMainRepo(project string) (string, error) {
	worktrees, err := p.store.ListWorktrees(project)
	if err != nil {
		return "", err
	}

	for _, wt := range worktrees {
		if wt.IsMain {
			return wt.Path, nil
		}
	}

	return "", fmt.Errorf("no main repo found for project: %s", project)
}

func (p *Provisioner) generateWorktreeName(project string, job *store.Job) string {
	// Extract issue ID if present (e.g., "ENG-123" from job)
	issueID := extractIssueID(job.NormalizedInput)
	if issueID != "" {
		return fmt.Sprintf("%s-%s", project, strings.ToLower(issueID))
	}

	// Use job ID prefix
	shortID := job.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("%s-%s", project, shortID)
}

func (p *Provisioner) generateBranchName(job *store.Job) string {
	// Try to extract issue ID
	issueID := extractIssueID(job.NormalizedInput)

	// Get username from git config
	username := getGitUsername()

	// Create slug from normalized input
	slug := slugify(job.NormalizedInput, 30)

	if issueID != "" {
		return fmt.Sprintf("%s/%s-%s", username, strings.ToLower(issueID), slug)
	}

	shortID := job.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("%s/%s-%s", username, shortID, slug)
}

func (p *Provisioner) isBranchMerged(mainRepo, branch string) bool {
	defaultBranch := p.getDefaultBranch(mainRepo)

	cmd := exec.Command("git", "branch", "--merged", defaultBranch)
	cmd.Dir = mainRepo
	output, _ := cmd.Output()

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		if line == branch {
			return true
		}
	}
	return false
}

func (p *Provisioner) getDefaultBranch(repoPath string) string {
	// Try to get from remote
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(output))
		return strings.TrimPrefix(branch, "origin/")
	}

	// Fall back to checking if main or master exists
	for _, branch := range []string{"main", "master"} {
		cmd = exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		cmd.Dir = repoPath
		if cmd.Run() == nil {
			return branch
		}
	}

	return "main"
}

func isAgentActive(status store.AgentStatus) bool {
	switch status {
	case store.AgentStatusRunning, store.AgentStatusPlanning,
		store.AgentStatusExecuting, store.AgentStatusSpawning,
		store.AgentStatusAttached:
		return true
	}
	return false
}

var issueIDPattern = regexp.MustCompile(`(?i)\b([A-Z]+-\d+)\b`)

func extractIssueID(text string) string {
	matches := issueIDPattern.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func getGitUsername() string {
	cmd := exec.Command("git", "config", "user.name")
	output, err := cmd.Output()
	if err != nil {
		return "user"
	}
	name := strings.TrimSpace(string(output))
	// Convert to slug-friendly format
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func slugify(text string, maxLen int) string {
	// Convert to lowercase
	text = strings.ToLower(text)

	// Replace non-alphanumeric with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	text = reg.ReplaceAllString(text, "-")

	// Remove leading/trailing hyphens
	text = strings.Trim(text, "-")

	// Truncate
	if len(text) > maxLen {
		text = text[:maxLen]
		// Don't end with a hyphen
		text = strings.TrimRight(text, "-")
	}

	return text
}

// NormalizePlan describes what normalize would do without making changes.
type NormalizePlan struct {
	BaseDir string       // Target directory for normalized repos
	Moves   []NormalizeMove
}

// NormalizeMove describes a single repo movement.
type NormalizeMove struct {
	Project     string
	CurrentPath string
	TargetPath  string
	IsMain      bool
	Worktrees   []WorktreeMove // Associated worktrees that need to move
}

// WorktreeMove describes a worktree that needs to be relocated.
type WorktreeMove struct {
	CurrentPath string
	TargetPath  string
	Branch      string
}

// PlanNormalize creates a plan for normalizing repos without making changes.
// The base_dir is the first directory in config.Repos.BaseDirs.
// Structure: ~/repos/<project>/ (main), ~/repos/<project>-<branch>/ (worktrees)
func (p *Provisioner) PlanNormalize() (*NormalizePlan, error) {
	if len(p.config.Repos.BaseDirs) == 0 {
		return nil, fmt.Errorf("no base_dirs configured")
	}

	baseDir := expandPath(p.config.Repos.BaseDirs[0])
	plan := &NormalizePlan{BaseDir: baseDir}

	// Get all worktrees grouped by project
	projects := make(map[string][]*store.Worktree)
	allWorktrees, err := p.store.ListWorktrees("")
	if err != nil {
		return nil, err
	}
	for _, wt := range allWorktrees {
		projects[wt.Project] = append(projects[wt.Project], wt)
	}

	// For each project, determine what needs to move
	for project, worktrees := range projects {
		// Find main repo
		var mainWt *store.Worktree
		var featureWts []*store.Worktree
		for _, wt := range worktrees {
			if wt.IsMain {
				mainWt = wt
			} else {
				featureWts = append(featureWts, wt)
			}
		}

		if mainWt == nil {
			continue // No main repo, skip
		}

		// Canonical paths
		mainTargetPath := filepath.Join(baseDir, project)

		// Check if main needs to move
		if mainWt.Path != mainTargetPath {
			move := NormalizeMove{
				Project:     project,
				CurrentPath: mainWt.Path,
				TargetPath:  mainTargetPath,
				IsMain:      true,
			}

			// Check each worktree
			for _, wt := range featureWts {
				wtName := filepath.Base(wt.Path)
				// Keep worktree name, just change parent dir
				wtTargetPath := filepath.Join(baseDir, wtName)
				if wt.Path != wtTargetPath {
					move.Worktrees = append(move.Worktrees, WorktreeMove{
						CurrentPath: wt.Path,
						TargetPath:  wtTargetPath,
						Branch:      wt.Branch,
					})
				}
			}

			plan.Moves = append(plan.Moves, move)
		} else {
			// Main is in place, but check worktrees
			var worktreeMoves []WorktreeMove
			for _, wt := range featureWts {
				wtName := filepath.Base(wt.Path)
				wtTargetPath := filepath.Join(baseDir, wtName)
				if wt.Path != wtTargetPath {
					worktreeMoves = append(worktreeMoves, WorktreeMove{
						CurrentPath: wt.Path,
						TargetPath:  wtTargetPath,
						Branch:      wt.Branch,
					})
				}
			}
			if len(worktreeMoves) > 0 {
				plan.Moves = append(plan.Moves, NormalizeMove{
					Project:     project,
					CurrentPath: mainWt.Path,
					TargetPath:  mainTargetPath,
					IsMain:      true,
					Worktrees:   worktreeMoves,
				})
			}
		}
	}

	return plan, nil
}

// Normalize moves repos to follow Athena's standard structure.
// Returns list of paths that were moved.
func (p *Provisioner) Normalize(dryRun bool) ([]string, error) {
	plan, err := p.PlanNormalize()
	if err != nil {
		return nil, err
	}

	if dryRun {
		var paths []string
		for _, move := range plan.Moves {
			if move.CurrentPath != move.TargetPath {
				paths = append(paths, fmt.Sprintf("%s -> %s", move.CurrentPath, move.TargetPath))
			}
			for _, wt := range move.Worktrees {
				paths = append(paths, fmt.Sprintf("%s -> %s", wt.CurrentPath, wt.TargetPath))
			}
		}
		return paths, nil
	}

	// Ensure base dir exists
	if err := os.MkdirAll(plan.BaseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base dir: %w", err)
	}

	var moved []string
	for _, move := range plan.Moves {
		// Move main repo first (worktrees reference it)
		if move.CurrentPath != move.TargetPath {
			if err := os.Rename(move.CurrentPath, move.TargetPath); err != nil {
				return moved, fmt.Errorf("failed to move %s: %w", move.CurrentPath, err)
			}
			// Update store
			p.store.UpdateWorktreePath(move.CurrentPath, move.TargetPath)
			moved = append(moved, move.TargetPath)
		}

		// Move worktrees using git worktree move
		for _, wt := range move.Worktrees {
			cmd := exec.Command("git", "worktree", "move", wt.CurrentPath, wt.TargetPath)
			cmd.Dir = move.TargetPath // Use the (possibly new) main repo path
			if output, err := cmd.CombinedOutput(); err != nil {
				return moved, fmt.Errorf("failed to move worktree %s: %s: %w", wt.CurrentPath, string(output), err)
			}
			// Update store
			p.store.UpdateWorktreePath(wt.CurrentPath, wt.TargetPath)
			moved = append(moved, wt.TargetPath)
		}
	}

	return moved, nil
}

