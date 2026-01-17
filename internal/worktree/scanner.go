// Package worktree handles git worktree discovery and management.
package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/store"
)

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
		// Store main repo
		wt := &store.Worktree{
			Path:    repo.Path,
			Project: repo.Project,
			Branch:  repo.Branch,
			IsMain:  repo.IsMain,
		}
		if err := s.store.UpsertWorktree(wt); err != nil {
			continue
		}

		// Store worktrees
		for _, worktree := range repo.Worktrees {
			wt := &store.Worktree{
				Path:    worktree.Path,
				Project: repo.Project,
				Branch:  worktree.Branch,
				IsMain:  false,
			}
			if err := s.store.UpsertWorktree(wt); err != nil {
				continue
			}
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
