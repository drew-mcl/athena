// Package worktree handles git worktree discovery and management.
package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/store"
)

// Ticket ID patterns to extract from branch names
// Supports: ENG-123, ATH-456, PROJ-789, etc.
var ticketIDPattern = regexp.MustCompile(`(?i)([A-Z]+-\d+)`)

// Scanner discovers git repositories and worktrees.
type Scanner struct {
	config *config.Config
	store  *store.Store
}

// NewScanner creates a new worktree scanner.
func NewScanner(cfg *config.Config, st *store.Store) *Scanner {
	return &Scanner{config: cfg, store: st}
}

// DiscoveredRepo represents a discovered git repository.
type DiscoveredRepo struct {
	Path      string
	Project   string
	IsMain    bool
	Branch    string
	Worktrees []DiscoveredWorktree
}

// DiscoveredWorktree represents a git worktree.
type DiscoveredWorktree struct {
	Path   string
	Branch string
}

// Scan discovers all git repositories and worktrees in configured directories.
func (s *Scanner) Scan() ([]DiscoveredRepo, error) {
	var repos []DiscoveredRepo
	seen := make(map[string]bool)

	// Scan base directories
	for _, baseDir := range s.config.Repos.BaseDirs {
		expanded := expandPath(baseDir)
		discovered, err := s.scanDirectory(expanded, seen)
		if err != nil {
			continue // Log but don't fail on individual directory errors
		}
		repos = append(repos, discovered...)
	}

	// Add explicitly included repos
	for _, includePath := range s.config.Repos.Include {
		expanded := expandPath(includePath)
		if seen[expanded] {
			continue
		}
		if repo, ok := s.scanRepo(expanded); ok {
			repos = append(repos, repo)
			seen[expanded] = true
		}
	}

	return repos, nil
}

// ScanAndStore discovers worktrees and persists them to the store.
func (s *Scanner) ScanAndStore() error {
	repos, err := s.Scan()
	if err != nil {
		return err
	}

	for _, repo := range repos {
		// Get project name from git remote (cached)
		projectName := GetProjectNameFromRemote(repo.Path)
		var projectNamePtr *string
		if projectName != "" {
			projectNamePtr = &projectName
		}

		// Store main repo
		wt := &store.Worktree{
			Path:        repo.Path,
			Project:     repo.Project,
			Branch:      repo.Branch,
			IsMain:      repo.IsMain,
			ProjectName: projectNamePtr,
			Status:      store.WorktreeStatusActive,
		}
		if err := s.store.UpsertWorktree(wt); err != nil {
			continue
		}

		// Store worktrees
		for _, worktree := range repo.Worktrees {
			// Extract ticket ID from branch name
			ticketID := ExtractTicketID(worktree.Branch)
			var ticketIDPtr *string
			if ticketID != "" {
				ticketIDPtr = &ticketID
			}

			// Extract hash from path if it follows our naming convention
			ticketHash := ExtractTicketHashFromPath(worktree.Path)
			var ticketHashPtr *string
			if ticketHash != "" {
				ticketHashPtr = &ticketHash
			}

			// Check if branch is merged (stale detection)
			status := store.WorktreeStatusActive
			if isBranchMerged(repo.Path, worktree.Branch) {
				status = store.WorktreeStatusMerged
			}

			wt := &store.Worktree{
				Path:        worktree.Path,
				Project:     repo.Project,
				Branch:      worktree.Branch,
				IsMain:      false,
				TicketID:    ticketIDPtr,
				TicketHash:  ticketHashPtr,
				ProjectName: projectNamePtr,
				Status:      status,
			}
			if err := s.store.UpsertWorktree(wt); err != nil {
				continue
			}
		}
	}

	// Also scan the dedicated worktree directory
	if s.config.Repos.WorktreeDir != "" {
		if err := s.scanWorktreeDir(); err != nil {
			// Log but don't fail - worktree dir may not exist yet
			_ = err
		}
	}

	return nil
}

// scanWorktreeDir scans the dedicated worktree directory.
func (s *Scanner) scanWorktreeDir() error {
	wtDir := expandPath(s.config.Repos.WorktreeDir)

	// Check if directory exists
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		return nil // Not an error, just nothing to scan
	}

	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(wtDir, entry.Name())

		// Check if this is a git worktree
		gitDir := filepath.Join(path, ".git")
		info, err := os.Stat(gitDir)
		if err != nil {
			continue
		}

		// .git file (not directory) indicates a worktree
		if info.IsDir() {
			continue // This is a main repo, not a worktree
		}

		branch := getCurrentBranch(path)
		projectName := GetProjectNameFromRemote(path)

		// Extract ticket ID from directory name or branch
		dirName := entry.Name()
		ticketID := ExtractTicketID(dirName)
		if ticketID == "" {
			ticketID = ExtractTicketID(branch)
		}

		// Extract hash from directory name
		ticketHash := ExtractTicketHashFromPath(path)

		// Build pointers for optional fields
		var ticketIDPtr, ticketHashPtr, projectNamePtr *string
		if ticketID != "" {
			ticketIDPtr = &ticketID
		}
		if ticketHash != "" {
			ticketHashPtr = &ticketHash
		}
		if projectName != "" {
			projectNamePtr = &projectName
		}

		// Determine project from projectName or fall back to dirName
		project := projectName
		if project == "" {
			project = dirName
		}

		// Check if branch is merged (stale detection)
		status := store.WorktreeStatusActive
		mainRepo := getMainRepoFromWorktree(path)
		if mainRepo != "" && isBranchMerged(mainRepo, branch) {
			status = store.WorktreeStatusMerged
		}

		wt := &store.Worktree{
			Path:        path,
			Project:     project,
			Branch:      branch,
			IsMain:      false,
			TicketID:    ticketIDPtr,
			TicketHash:  ticketHashPtr,
			ProjectName: projectNamePtr,
			Status:      status,
		}
		if err := s.store.UpsertWorktree(wt); err != nil {
			continue
		}
	}

	return nil
}

func (s *Scanner) scanDirectory(dir string, seen map[string]bool) ([]DiscoveredRepo, error) {
	var repos []DiscoveredRepo

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		// Check exclude patterns
		if s.isExcluded(path) {
			continue
		}

		if seen[path] {
			continue
		}

		// Check if this is a git repo
		if repo, ok := s.scanRepo(path); ok {
			repos = append(repos, repo)
			seen[path] = true
		}
	}

	return repos, nil
}

func (s *Scanner) scanRepo(path string) (DiscoveredRepo, bool) {
	gitDir := filepath.Join(path, ".git")

	// Check if .git exists (could be file for worktree or directory for main repo)
	info, err := os.Stat(gitDir)
	if err != nil {
		return DiscoveredRepo{}, false
	}

	project := filepath.Base(path)
	branch := getCurrentBranch(path)

	repo := DiscoveredRepo{
		Path:    path,
		Project: project,
		Branch:  branch,
		IsMain:  info.IsDir(), // .git directory = main repo, .git file = worktree
	}

	// If it's a main repo, find its worktrees
	if info.IsDir() {
		repo.Worktrees = s.findWorktrees(path)
	}

	return repo, true
}

func (s *Scanner) findWorktrees(mainRepoPath string) []DiscoveredWorktree {
	var worktrees []DiscoveredWorktree

	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = mainRepoPath
	output, err := cmd.Output()
	if err != nil {
		return worktrees
	}

	var currentPath, currentBranch string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "worktree ") {
			// Save previous worktree if we have one
			if currentPath != "" && currentPath != mainRepoPath {
				worktrees = append(worktrees, DiscoveredWorktree{
					Path:   currentPath,
					Branch: currentBranch,
				})
			}
			currentPath = strings.TrimPrefix(line, "worktree ")
			currentBranch = ""
		} else if strings.HasPrefix(line, "branch ") {
			currentBranch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}

	// Don't forget the last one
	if currentPath != "" && currentPath != mainRepoPath {
		worktrees = append(worktrees, DiscoveredWorktree{
			Path:   currentPath,
			Branch: currentBranch,
		})
	}

	return worktrees
}

func (s *Scanner) isExcluded(path string) bool {
	for _, pattern := range s.config.Repos.Exclude {
		// Simple glob matching - could use filepath.Match for more complex patterns
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
		// Check if pattern matches any part of the path
		if strings.Contains(path, strings.Trim(pattern, "*")) {
			return true
		}
	}
	return false
}

func getCurrentBranch(repoPath string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// ExtractTicketID extracts a ticket ID from a branch name.
// Supports patterns like: drew/eng-123-feature, ENG-123-feature, eng-123
func ExtractTicketID(branch string) string {
	matches := ticketIDPattern.FindStringSubmatch(branch)
	if len(matches) >= 2 {
		return strings.ToUpper(matches[1])
	}
	return ""
}

// GetProjectNameFromRemote extracts the project name from git remote URL.
// Supports: git@github.com:user/project.git, https://github.com/user/project.git
func GetProjectNameFromRemote(repoPath string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	remote := strings.TrimSpace(string(output))
	return parseProjectFromRemote(remote)
}

// parseProjectFromRemote parses a git remote URL to extract the project name.
func parseProjectFromRemote(remote string) string {
	// Remove trailing .git
	remote = strings.TrimSuffix(remote, ".git")

	// Handle SSH format: git@github.com:user/project
	if strings.Contains(remote, ":") && strings.Contains(remote, "@") {
		parts := strings.Split(remote, ":")
		if len(parts) == 2 {
			pathParts := strings.Split(parts[1], "/")
			if len(pathParts) >= 1 {
				return pathParts[len(pathParts)-1]
			}
		}
	}

	// Handle HTTPS format: https://github.com/user/project
	parts := strings.Split(remote, "/")
	if len(parts) >= 1 {
		return parts[len(parts)-1]
	}

	return ""
}

// ExtractTicketHashFromPath extracts the 4-char hash from worktree directory name.
// Expected format: ENG-123-a1b2 or TICKET-ID-hash
func ExtractTicketHashFromPath(path string) string {
	name := filepath.Base(path)
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		// Last part should be the hash
		hash := parts[len(parts)-1]
		if len(hash) == 4 {
			return hash
		}
	}
	return ""
}

// isBranchMerged checks if a branch has been merged into the default branch.
func isBranchMerged(repoPath, branch string) bool {
	if branch == "" {
		return false
	}

	defaultBranch := getDefaultBranch(repoPath)

	cmd := exec.Command("git", "branch", "--merged", defaultBranch)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		if line == branch {
			return true
		}
	}
	return false
}

// getDefaultBranch determines the default branch for a repository (main or master).
func getDefaultBranch(repoPath string) string {
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

// getMainRepoFromWorktree extracts the main repository path from a worktree.
// Worktrees have a .git file (not directory) that points to the main repo.
func getMainRepoFromWorktree(worktreePath string) string {
	gitFile := filepath.Join(worktreePath, ".git")

	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}

	// Format: "gitdir: /path/to/main/.git/worktrees/name"
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return ""
	}

	gitDir := strings.TrimPrefix(content, "gitdir: ")

	// Navigate up from .git/worktrees/name to get main repo
	// gitDir is like: /path/to/main/.git/worktrees/wt-name
	parts := strings.Split(gitDir, string(filepath.Separator))
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == ".git" {
			// Reconstruct path up to .git parent
			mainPath := filepath.Join(parts[:i]...)
			if !filepath.IsAbs(mainPath) && len(parts) > 0 && parts[0] == "" {
				// Unix absolute path - add leading /
				mainPath = "/" + mainPath
			}
			return mainPath
		}
	}

	return ""
}
