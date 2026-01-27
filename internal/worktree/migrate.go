// Package worktree handles git worktree discovery and management.
package worktree

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/executil"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

// Migrator handles migration of worktrees to the new structure.
type Migrator struct {
	config *config.Config
	store  *store.Store
}

// NewMigrator creates a new worktree migrator.
func NewMigrator(cfg *config.Config, st *store.Store) *Migrator {
	return &Migrator{config: cfg, store: st}
}

// MigrationPlan describes what migration would do.
type MigrationPlan struct {
	WorktreeDir string          `json:"worktree_dir"`
	Migrations  []MigrationItem `json:"migrations"`
}

// MigrationItem describes a single worktree migration.
type MigrationItem struct {
	CurrentPath string `json:"current_path"`
	TargetPath  string `json:"target_path"`
	Branch      string `json:"branch"`
	TicketID    string `json:"ticket_id"`
	Hash        string `json:"hash"`
	Project     string `json:"project"`
}

// PlanMigration identifies worktrees that need migration.
func (m *Migrator) PlanMigration() (*MigrationPlan, error) {
	worktrees, err := m.store.ListWorktrees("")
	if err != nil {
		return nil, err
	}

	wtDir := expandPath(m.config.Repos.WorktreeDir)

	plan := &MigrationPlan{
		WorktreeDir: wtDir,
	}

	for _, wt := range worktrees {
		// Skip main repos
		if wt.IsMain {
			continue
		}

		// Skip if already in the worktree directory
		if strings.HasPrefix(wt.Path, wtDir) {
			continue
		}

		// This worktree needs migration
		ticketID := ExtractTicketID(wt.Branch)
		if ticketID == "" {
			// Generate a placeholder ticket ID
			ticketID = fmt.Sprintf("WT-%d", generateNumericID())
		}

		hash := generateHash(4)
		targetName := fmt.Sprintf("%s-%s", ticketID, hash)
		targetPath := filepath.Join(wtDir, targetName)

		// Get project name
		project := wt.Project
		if wt.ProjectName != nil && *wt.ProjectName != "" {
			project = *wt.ProjectName
		}

		plan.Migrations = append(plan.Migrations, MigrationItem{
			CurrentPath: wt.Path,
			TargetPath:  targetPath,
			Branch:      wt.Branch,
			TicketID:    ticketID,
			Hash:        hash,
			Project:     project,
		})
	}

	return plan, nil
}

// Migrate moves worktrees to the new structure.
// If dryRun is true, only plans the migration without executing.
func (m *Migrator) Migrate(dryRun bool) ([]string, error) {
	plan, err := m.PlanMigration()
	if err != nil {
		return nil, err
	}

	if len(plan.Migrations) == 0 {
		return nil, nil
	}

	// Ensure worktree directory exists
	if !dryRun {
		if err := os.MkdirAll(plan.WorktreeDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create worktree directory: %w", err)
		}
	}

	var migrated []string
	for _, item := range plan.Migrations {
		if dryRun {
			migrated = append(migrated, fmt.Sprintf("%s -> %s", item.CurrentPath, item.TargetPath))
			continue
		}

		if err := m.migrateWorktree(item); err != nil {
			logging.Error("failed to migrate worktree",
				"path", item.CurrentPath,
				"error", err)
			continue
		}

		migrated = append(migrated, item.TargetPath)
	}

	return migrated, nil
}

// migrateWorktree moves a single worktree to the new location.
func (m *Migrator) migrateWorktree(item MigrationItem) error {
	logging.Info("migrating worktree",
		"from", item.CurrentPath,
		"to", item.TargetPath,
		"ticket", item.TicketID)

	// Step 1: Move the directory
	if err := os.Rename(item.CurrentPath, item.TargetPath); err != nil {
		return fmt.Errorf("failed to move directory: %w", err)
	}

	// Step 2: Update git worktree path
	// The .git file in worktrees contains a reference to the main repo
	// We need to update both sides of this relationship

	gitFilePath := filepath.Join(item.TargetPath, ".git")
	gitFileContent, err := os.ReadFile(gitFilePath)
	if err != nil {
		// Rollback: move directory back
		os.Rename(item.TargetPath, item.CurrentPath)
		return fmt.Errorf("failed to read .git file: %w", err)
	}

	// Parse the gitdir reference
	// Format: "gitdir: /path/to/main-repo/.git/worktrees/worktree-name"
	gitdirLine := strings.TrimSpace(string(gitFileContent))
	if !strings.HasPrefix(gitdirLine, "gitdir: ") {
		os.Rename(item.TargetPath, item.CurrentPath)
		return fmt.Errorf("unexpected .git file format")
	}

	gitdirPath := strings.TrimPrefix(gitdirLine, "gitdir: ")

	// Step 3: Update the worktree's gitdir file in the main repo
	// This file contains the path to the worktree
	worktreeGitDir := filepath.Join(gitdirPath, "gitdir")
	if err := os.WriteFile(worktreeGitDir, []byte(item.TargetPath+"\n"), 0644); err != nil {
		// Rollback
		os.Rename(item.TargetPath, item.CurrentPath)
		return fmt.Errorf("failed to update gitdir: %w", err)
	}

	// Step 4: Update database record
	ticketIDPtr := &item.TicketID
	hashPtr := &item.Hash

	// Update the path in the database
	if err := m.store.UpdateWorktreePath(item.CurrentPath, item.TargetPath); err != nil {
		logging.Warn("failed to update worktree path in database",
			"error", err)
	}

	// Update ticket metadata
	if err := m.store.UpdateWorktreeTicket(item.TargetPath, *ticketIDPtr, *hashPtr, ""); err != nil {
		logging.Warn("failed to update worktree ticket metadata",
			"error", err)
	}

	logging.Info("worktree migrated successfully",
		"path", item.TargetPath,
		"ticket", item.TicketID)

	return nil
}

// generateHash generates a random hex string of specified length.
func generateHash(length int) string {
	bytes := make([]byte, length/2+1)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}

// generateNumericID generates a short numeric ID for placeholder tickets.
func generateNumericID() int64 {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return int64(bytes[0])<<24 | int64(bytes[1])<<16 | int64(bytes[2])<<8 | int64(bytes[3])
}

// CreateWorktree creates a new worktree in the dedicated worktree directory.
type CreateWorktreeOptions struct {
	MainRepoPath string // Path to the main repository
	Branch       string // Branch name to checkout/create
	TicketID     string // Ticket ID (e.g., ENG-123)
	Description  string // Description of the work
	WorkflowMode string // automatic | approve | manual
	SourceNoteID string // Note ID if promoted from note (for abandon rollback)
}

// CreateWorktree creates a new worktree with the standard naming convention.
func (m *Migrator) CreateWorktree(opts CreateWorktreeOptions) (string, error) {
	wtDir := expandPath(m.config.Repos.WorktreeDir)

	// Ensure worktree directory exists
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create worktree directory: %w", err)
	}

	// Generate hash
	hash := generateHash(4)

	// Determine directory name
	ticketID := opts.TicketID
	if ticketID == "" {
		ticketID = fmt.Sprintf("WT-%d", generateNumericID())
	}
	dirName := fmt.Sprintf("%s-%s", ticketID, hash)
	targetPath := filepath.Join(wtDir, dirName)

	// Create the worktree using git
	branch := opts.Branch
	if branch == "" {
		// Create a branch based on ticket ID
		branch = fmt.Sprintf("feat/%s", strings.ToLower(ticketID))
	}

	// Check if branch exists
	cmd, err := executil.Command("git", "rev-parse", "--verify", branch)
	if err != nil {
		return "", err
	}
	cmd.Dir = opts.MainRepoPath
	branchExists := cmd.Run() == nil

	var gitArgs []string
	if branchExists {
		gitArgs = []string{"worktree", "add", targetPath, branch}
	} else {
		gitArgs = []string{"worktree", "add", "-b", branch, targetPath}
	}

	cmd, err = executil.Command("git", gitArgs...)
	if err != nil {
		return "", err
	}
	cmd.Dir = opts.MainRepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}

	// Get project name from main repo
	projectName := GetProjectNameFromRemote(opts.MainRepoPath)
	var projectNamePtr *string
	if projectName != "" {
		projectNamePtr = &projectName
	}

	// Store in database
	ticketIDPtr := &ticketID
	hashPtr := &hash
	var descPtr *string
	if opts.Description != "" {
		descPtr = &opts.Description
	}

	// Default workflow mode to approve if not set
	workflowMode := opts.WorkflowMode
	if workflowMode == "" {
		workflowMode = "approve"
	}
	workflowModePtr := &workflowMode

	// Set source note ID if provided
	var sourceNoteIDPtr *string
	if opts.SourceNoteID != "" {
		sourceNoteIDPtr = &opts.SourceNoteID
	}

	wt := &store.Worktree{
		Path:         targetPath,
		Project:      projectName,
		Branch:       branch,
		IsMain:       false,
		TicketID:     ticketIDPtr,
		TicketHash:   hashPtr,
		Description:  descPtr,
		ProjectName:  projectNamePtr,
		Status:       store.WorktreeStatusActive,
		WorkflowMode: workflowModePtr,
		SourceNoteID: sourceNoteIDPtr,
	}

	if err := m.store.UpsertWorktree(wt); err != nil {
		logging.Warn("failed to store worktree", "error", err)
	}

	// Create .todo file
	if opts.Description != "" || opts.TicketID != "" {
		if err := m.createTodoFile(targetPath, opts); err != nil {
			logging.Warn("failed to create .todo file", "error", err)
		}
	}

	return targetPath, nil
}

// createTodoFile creates a .todo file in the worktree root.
func (m *Migrator) createTodoFile(worktreePath string, opts CreateWorktreeOptions) error {
	todoPath := filepath.Join(worktreePath, ".todo")

	var content strings.Builder
	content.WriteString(fmt.Sprintf("# %s", opts.TicketID))
	if opts.Description != "" {
		content.WriteString(fmt.Sprintf(": %s", opts.Description))
	}
	content.WriteString("\n\n")
	content.WriteString("## Description\n")
	if opts.Description != "" {
		content.WriteString(opts.Description)
	} else {
		if opts.TicketID != "" {
			content.WriteString(fmt.Sprintf("Work for %s", opts.TicketID))
		} else {
			content.WriteString(fmt.Sprintf("Feature work on %s", opts.Branch))
		}
	}
	content.WriteString("\n\n")
	content.WriteString("## Tasks\n")
	content.WriteString("- [ ] Set up development environment\n")
	content.WriteString("- [ ] Explore codebase structure\n")
	if opts.TicketID != "" {
		content.WriteString(fmt.Sprintf("- [ ] Review requirements for %s\n", opts.TicketID))
	} else {
		content.WriteString("- [ ] Define implementation approach\n")
	}
	content.WriteString("- [ ] Create implementation plan\n")
	content.WriteString("\n---\n")
	content.WriteString(fmt.Sprintf("Ticket: %s\n", opts.TicketID))

	return os.WriteFile(todoPath, []byte(content.String()), 0644)
}
