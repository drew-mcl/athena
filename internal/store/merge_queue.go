// Package store provides the merge queue persistence layer.
package store

import (
	"database/sql"
	"fmt"
	"time"
)

// MergeQueueStatus represents the state of a worktree in the merge queue.
type MergeQueueStatus string

const (
	MergeQueueStatusQueued   MergeQueueStatus = "queued"   // Waiting in line
	MergeQueueStatusMerging  MergeQueueStatus = "merging"  // Currently being merged to main
	MergeQueueStatusMerged   MergeQueueStatus = "merged"   // Successfully merged
	MergeQueueStatusConflict MergeQueueStatus = "conflict" // Needs manual resolution
	MergeQueueStatusRebasing MergeQueueStatus = "rebasing" // Being rebased after edit
	MergeQueueStatusDiverged MergeQueueStatus = "diverged" // Upstream changed; needs reconciliation
)

// MergeQueueItem represents a worktree in the merge queue.
type MergeQueueItem struct {
	ID           string           // Unique ID
	Project      string           // Project name
	WorktreePath string           // Path to the worktree
	Branch       string           // Branch name
	Position     int              // Queue position (1 = next to merge)
	Status       MergeQueueStatus // Current status
	BaseBranch   string           // Branch this was based on (main or another queue item's branch)
	BaseCommit   string           // Commit SHA this was based on
	HeadCommit   string           // Current HEAD commit of this branch
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// migrateMergeQueue creates the merge queue table if it doesn't exist.
func (s *Store) migrateMergeQueue() error {
	schema := `
	CREATE TABLE IF NOT EXISTS merge_queue (
		id TEXT PRIMARY KEY,
		project TEXT NOT NULL,
		worktree_path TEXT NOT NULL UNIQUE,
		branch TEXT NOT NULL,
		position INTEGER NOT NULL,
		status TEXT DEFAULT 'queued',
		base_branch TEXT NOT NULL,
		base_commit TEXT NOT NULL,
		head_commit TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (worktree_path) REFERENCES worktrees(path)
	);

	CREATE INDEX IF NOT EXISTS idx_merge_queue_project ON merge_queue(project);
	CREATE INDEX IF NOT EXISTS idx_merge_queue_position ON merge_queue(project, position);
	CREATE INDEX IF NOT EXISTS idx_merge_queue_status ON merge_queue(status);
	`
	_, err := s.db.Exec(schema)
	return err
}

// AddToMergeQueue adds a worktree to the merge queue at the back.
func (s *Store) AddToMergeQueue(item *MergeQueueItem) error {
	// Get the next position for this project
	var maxPos sql.NullInt64
	err := s.db.QueryRow(`
		SELECT MAX(position) FROM merge_queue
		WHERE project = ? AND status IN ('queued', 'rebasing', 'diverged', 'conflict')
	`, item.Project).Scan(&maxPos)
	if err != nil {
		return fmt.Errorf("failed to get max position: %w", err)
	}

	nextPos := 1
	if maxPos.Valid {
		nextPos = int(maxPos.Int64) + 1
	}

	item.Position = nextPos
	item.Status = MergeQueueStatusQueued

	_, err = s.db.Exec(`
		INSERT INTO merge_queue (id, project, worktree_path, branch, position, status, base_branch, base_commit, head_commit, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, item.ID, item.Project, item.WorktreePath, item.Branch, item.Position, item.Status, item.BaseBranch, item.BaseCommit, item.HeadCommit)
	if err != nil {
		return fmt.Errorf("failed to insert queue item: %w", err)
	}

	return nil
}

// GetMergeQueue returns all items in the merge queue for a project, ordered by position.
func (s *Store) GetMergeQueue(project string) ([]*MergeQueueItem, error) {
	rows, err := s.db.Query(`
		SELECT id, project, worktree_path, branch, position, status, base_branch, base_commit, head_commit, created_at, updated_at
		FROM merge_queue
		WHERE project = ? AND status IN ('queued', 'rebasing', 'merging', 'diverged', 'conflict')
		ORDER BY position ASC
	`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*MergeQueueItem
	for rows.Next() {
		item := &MergeQueueItem{}
		var headCommit sql.NullString
		err := rows.Scan(
			&item.ID, &item.Project, &item.WorktreePath, &item.Branch,
			&item.Position, &item.Status, &item.BaseBranch, &item.BaseCommit,
			&headCommit, &item.CreatedAt, &item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if headCommit.Valid {
			item.HeadCommit = headCommit.String
		}
		items = append(items, item)
	}
	return items, nil
}

// GetMergeQueueItem returns a specific queue item by worktree path.
func (s *Store) GetMergeQueueItem(worktreePath string) (*MergeQueueItem, error) {
	item := &MergeQueueItem{}
	var headCommit sql.NullString
	err := s.db.QueryRow(`
		SELECT id, project, worktree_path, branch, position, status, base_branch, base_commit, head_commit, created_at, updated_at
		FROM merge_queue
		WHERE worktree_path = ?
	`, worktreePath).Scan(
		&item.ID, &item.Project, &item.WorktreePath, &item.Branch,
		&item.Position, &item.Status, &item.BaseBranch, &item.BaseCommit,
		&headCommit, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if headCommit.Valid {
		item.HeadCommit = headCommit.String
	}
	return item, nil
}

// GetQueueHead returns the integration HEAD - the branch/commit that new worktrees should base on.
// It walks the queue from the front and returns the last non-diverged item.
func (s *Store) GetQueueHead(project string) (branch string, commit string, err error) {
	rows, err := s.db.Query(`
		SELECT branch, head_commit, status
		FROM merge_queue
		WHERE project = ? AND status IN ('queued', 'rebasing', 'merging', 'diverged', 'conflict')
		ORDER BY position ASC
	`, project)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()

	for rows.Next() {
		var itemBranch sql.NullString
		var headCommit sql.NullString
		var status MergeQueueStatus
		if err := rows.Scan(&itemBranch, &headCommit, &status); err != nil {
			return "", "", err
		}

		// Divergence means the chain beyond this point is no longer a valid integration base.
		if status == MergeQueueStatusDiverged || status == MergeQueueStatusConflict {
			break
		}
		if itemBranch.Valid {
			branch = itemBranch.String
		}
		if headCommit.Valid {
			commit = headCommit.String
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	return branch, commit, nil
}

// UpdateMergeQueueItem updates a queue item's status and optionally head commit.
func (s *Store) UpdateMergeQueueItem(worktreePath string, status MergeQueueStatus, headCommit string) error {
	if headCommit != "" {
		_, err := s.db.Exec(`
			UPDATE merge_queue
			SET status = ?, head_commit = ?, updated_at = CURRENT_TIMESTAMP
			WHERE worktree_path = ?
		`, status, headCommit, worktreePath)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE merge_queue
		SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE worktree_path = ?
	`, status, worktreePath)
	return err
}

// RemoveFromMergeQueue removes a worktree from the queue and reorders positions.
func (s *Store) RemoveFromMergeQueue(worktreePath string) error {
	// Get the item first to know its position
	item, err := s.GetMergeQueueItem(worktreePath)
	if err != nil {
		return err
	}
	if item == nil {
		return nil // Not in queue
	}

	// Delete the item
	_, err = s.db.Exec(`DELETE FROM merge_queue WHERE worktree_path = ?`, worktreePath)
	if err != nil {
		return err
	}

	// Reorder: decrement positions for items after the removed one
	_, err = s.db.Exec(`
		UPDATE merge_queue
		SET position = position - 1, updated_at = CURRENT_TIMESTAMP
		WHERE project = ? AND position > ?
	`, item.Project, item.Position)
	return err
}

// MoveToBackOfQueue records an edited queue item without changing its position.
// Items behind it are marked as diverged so they can be reconciled.
func (s *Store) MoveToBackOfQueue(worktreePath string, newBaseCommit string) error {
	item, err := s.GetMergeQueueItem(worktreePath)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("worktree not in queue: %s", worktreePath)
	}

	originalPosition := item.Position

	// Keep this item in-place and update its latest head commit.
	_, err = s.db.Exec(`
		UPDATE merge_queue
		SET head_commit = ?, status = 'queued', updated_at = CURRENT_TIMESTAMP
		WHERE worktree_path = ?
	`, newBaseCommit, worktreePath)
	if err != nil {
		return err
	}

	// Downstream items no longer cleanly stack and must be reconciled.
	_, err = s.db.Exec(`
		UPDATE merge_queue
		SET status = 'diverged', updated_at = CURRENT_TIMESTAMP
		WHERE project = ? AND position > ? AND worktree_path != ?
	`, item.Project, originalPosition, worktreePath)
	return err
}

// GetItemsNeedingRebase returns queue items that need to be rebased.
func (s *Store) GetItemsNeedingRebase(project string) ([]*MergeQueueItem, error) {
	rows, err := s.db.Query(`
		SELECT id, project, worktree_path, branch, position, status, base_branch, base_commit, head_commit, created_at, updated_at
		FROM merge_queue
		WHERE project = ? AND status IN ('rebasing', 'diverged')
		ORDER BY position ASC
	`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*MergeQueueItem
	for rows.Next() {
		item := &MergeQueueItem{}
		var headCommit sql.NullString
		err := rows.Scan(
			&item.ID, &item.Project, &item.WorktreePath, &item.Branch,
			&item.Position, &item.Status, &item.BaseBranch, &item.BaseCommit,
			&headCommit, &item.CreatedAt, &item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if headCommit.Valid {
			item.HeadCommit = headCommit.String
		}
		items = append(items, item)
	}
	return items, nil
}

// MarkQueueItemRebased updates a queue item after successful rebase.
func (s *Store) MarkQueueItemRebased(worktreePath string, newBaseCommit, newHeadCommit string) error {
	return s.MarkQueueItemRebasedWithBase(worktreePath, "", newBaseCommit, newHeadCommit)
}

// MarkQueueItemRebasedWithBase updates queue metadata after successful rebase.
func (s *Store) MarkQueueItemRebasedWithBase(worktreePath, newBaseBranch, newBaseCommit, newHeadCommit string) error {
	if newBaseBranch != "" {
		_, err := s.db.Exec(`
		UPDATE merge_queue
		SET status = 'queued', base_branch = ?, base_commit = ?, head_commit = ?, updated_at = CURRENT_TIMESTAMP
		WHERE worktree_path = ?
	`, newBaseBranch, newBaseCommit, newHeadCommit, worktreePath)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE merge_queue
		SET status = 'queued', base_commit = ?, head_commit = ?, updated_at = CURRENT_TIMESTAMP
		WHERE worktree_path = ?
	`, newBaseCommit, newHeadCommit, worktreePath)
	return err
}

// MarkQueueItemsDiverged marks queue items at or after a position as diverged.
func (s *Store) MarkQueueItemsDiverged(project string, startPosition int) error {
	_, err := s.db.Exec(`
		UPDATE merge_queue
		SET status = 'diverged', updated_at = CURRENT_TIMESTAMP
		WHERE project = ? AND position >= ? AND status IN ('queued', 'rebasing', 'diverged')
	`, project, startPosition)
	return err
}

// GetActiveQueueProjects returns a list of projects that have items in the merge queue.
func (s *Store) GetActiveQueueProjects() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT project FROM merge_queue
		WHERE status IN ('queued', 'rebasing', 'merging', 'diverged', 'conflict')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			projects = append(projects, p)
		}
	}
	return projects, nil
}
