package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GenerateWorkItemID creates a hierarchical ID for a work item.
// Goals get: wi-<4char hash>
// Children get: parent.sequence (e.g., wi-a3f8.1, wi-a3f8.1.2)
func (s *Store) GenerateWorkItemID(parentID string) (string, error) {
	if parentID == "" {
		// Root level (goal): wi-<4char hash>
		return "wi-" + shortHash(), nil
	}

	// Child: parent.sequence
	seq, err := s.getNextChildSequence(parentID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%d", parentID, seq), nil
}

// shortHash generates a 4-character random hex string.
func shortHash() string {
	b := make([]byte, 2)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// getNextChildSequence returns the next sequence number for a parent's children.
func (s *Store) getNextChildSequence(parentID string) (int, error) {
	query := `SELECT id FROM work_items WHERE parent_id = ? ORDER BY id DESC LIMIT 1`
	row := s.db.QueryRow(query, parentID)

	var lastID string
	err := row.Scan(&lastID)
	if err == sql.ErrNoRows {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}

	// Extract the last sequence number from the ID
	parts := strings.Split(lastID, ".")
	if len(parts) == 0 {
		return 1, nil
	}
	lastSeq, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 1, nil
	}
	return lastSeq + 1, nil
}

// CreateWorkItem inserts a new work item.
func (s *Store) CreateWorkItem(item *WorkItem) error {
	query := `
		INSERT INTO work_items (
			id, project, item_type, parent_id, subject, description, status,
			worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	now := time.Now()
	_, err := s.db.Exec(query,
		item.ID, item.Project, item.ItemType, item.ParentID,
		item.Subject, item.Description, item.Status,
		item.WorktreePath, item.TicketID, item.PRURL,
		item.AgentID, item.Priority, item.Metadata,
		now, now,
	)
	if err != nil {
		return err
	}

	item.CreatedAt = now
	item.UpdatedAt = now
	return nil
}

// GetWorkItem retrieves a work item by ID.
func (s *Store) GetWorkItem(id string) (*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM work_items WHERE id = ?`

	row := s.db.QueryRow(query, id)
	return scanWorkItem(row)
}

// ListWorkItems retrieves work items with optional filters.
func (s *Store) ListWorkItems(project string, itemType WorkItemType, status WorkItemStatus) ([]*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM work_items WHERE 1=1`

	args := []any{}

	if project != "" {
		query += " AND project = ?"
		args = append(args, project)
	}
	if itemType != "" {
		query += " AND item_type = ?"
		args = append(args, itemType)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// ListWorkItemsByParent retrieves direct children of a work item.
func (s *Store) ListWorkItemsByParent(parentID string) ([]*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM work_items WHERE parent_id = ?
		ORDER BY priority ASC, created_at ASC`

	rows, err := s.db.Query(query, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// ListOrphanTasks retrieves tasks with no parent (inbox items).
func (s *Store) ListOrphanTasks(project string) ([]*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM work_items
		WHERE parent_id IS NULL AND item_type = 'task'`

	args := []any{}
	if project != "" {
		query += " AND project = ?"
		args = append(args, project)
	}
	query += " ORDER BY priority ASC, created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// ListReadyItems retrieves items that are unblocked (no incomplete children).
func (s *Store) ListReadyItems(project string) ([]*WorkItem, error) {
	// Items are "ready" if:
	// 1. They are pending status
	// 2. They have no children, OR all their children are completed
	query := `
		SELECT w.id, w.project, w.item_type, w.parent_id, w.subject, w.description, w.status,
		       w.worktree_path, w.ticket_id, w.pr_url, w.agent_id, w.priority, w.metadata,
		       w.created_at, w.updated_at
		FROM work_items w
		WHERE w.status = 'pending'
		  AND NOT EXISTS (
		      SELECT 1 FROM work_items c
		      WHERE c.parent_id = w.id AND c.status != 'completed'
		  )`

	args := []any{}
	if project != "" {
		query += " AND w.project = ?"
		args = append(args, project)
	}
	query += " ORDER BY w.priority ASC, w.created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// GetWorkItemTree retrieves a work item and all its descendants.
func (s *Store) GetWorkItemTree(rootID string) ([]*WorkItem, error) {
	// Use recursive CTE to get all descendants
	query := `
		WITH RECURSIVE descendants AS (
			SELECT id, project, item_type, parent_id, subject, description, status,
			       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
			       created_at, updated_at, 0 as depth
			FROM work_items WHERE id = ?

			UNION ALL

			SELECT w.id, w.project, w.item_type, w.parent_id, w.subject, w.description, w.status,
			       w.worktree_path, w.ticket_id, w.pr_url, w.agent_id, w.priority, w.metadata,
			       w.created_at, w.updated_at, d.depth + 1
			FROM work_items w
			INNER JOIN descendants d ON w.parent_id = d.id
		)
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM descendants
		ORDER BY depth, priority ASC, created_at ASC`

	rows, err := s.db.Query(query, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// GetWorkItemAncestors retrieves all ancestors of a work item (for context view).
func (s *Store) GetWorkItemAncestors(itemID string) ([]*WorkItem, error) {
	// Use recursive CTE to get all ancestors
	query := `
		WITH RECURSIVE ancestors AS (
			SELECT id, project, item_type, parent_id, subject, description, status,
			       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
			       created_at, updated_at, 0 as depth
			FROM work_items WHERE id = ?

			UNION ALL

			SELECT w.id, w.project, w.item_type, w.parent_id, w.subject, w.description, w.status,
			       w.worktree_path, w.ticket_id, w.pr_url, w.agent_id, w.priority, w.metadata,
			       w.created_at, w.updated_at, a.depth + 1
			FROM work_items w
			INNER JOIN ancestors a ON w.id = a.parent_id
		)
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM ancestors
		ORDER BY depth DESC`

	rows, err := s.db.Query(query, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// GetWorkItemProgress calculates completion stats for a work item.
func (s *Store) GetWorkItemProgress(itemID string) (completed, total int, err error) {
	query := `
		WITH RECURSIVE descendants AS (
			SELECT id, status FROM work_items WHERE parent_id = ?

			UNION ALL

			SELECT w.id, w.status
			FROM work_items w
			INNER JOIN descendants d ON w.parent_id = d.id
		)
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed
		FROM descendants`

	row := s.db.QueryRow(query, itemID)
	err = row.Scan(&total, &completed)
	return
}

// UpdateWorkItem updates a work item's fields.
func (s *Store) UpdateWorkItem(item *WorkItem) error {
	query := `
		UPDATE work_items SET
			subject = ?, description = ?, status = ?,
			worktree_path = ?, ticket_id = ?, pr_url = ?,
			agent_id = ?, priority = ?, metadata = ?,
			updated_at = ?
		WHERE id = ?`

	now := time.Now()
	_, err := s.db.Exec(query,
		item.Subject, item.Description, item.Status,
		item.WorktreePath, item.TicketID, item.PRURL,
		item.AgentID, item.Priority, item.Metadata,
		now, item.ID,
	)
	if err != nil {
		return err
	}

	item.UpdatedAt = now
	return nil
}

// UpdateWorkItemStatus updates just the status field.
func (s *Store) UpdateWorkItemStatus(id string, status WorkItemStatus) error {
	query := `UPDATE work_items SET status = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, status, time.Now(), id)
	return err
}

// UpdateWorkItemAgent assigns an agent to a work item.
func (s *Store) UpdateWorkItemAgent(id string, agentID *string) error {
	query := `UPDATE work_items SET agent_id = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, agentID, time.Now(), id)
	return err
}

// DeleteWorkItem removes a work item (soft delete could be added later).
func (s *Store) DeleteWorkItem(id string) error {
	query := `DELETE FROM work_items WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// GetWorkItemByWorktree finds a feature work item by its worktree path.
func (s *Store) GetWorkItemByWorktree(worktreePath string) (*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM work_items WHERE worktree_path = ?`

	row := s.db.QueryRow(query, worktreePath)
	return scanWorkItem(row)
}

// GetWorkItemByTicket finds a work item by ticket ID.
func (s *Store) GetWorkItemByTicket(ticketID string) (*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
		       worktree_path, ticket_id, pr_url, agent_id, priority, metadata,
		       created_at, updated_at
		FROM work_items WHERE ticket_id = ?`

	row := s.db.QueryRow(query, ticketID)
	return scanWorkItem(row)
}

// Helper functions for scanning

func scanWorkItem(row *sql.Row) (*WorkItem, error) {
	var item WorkItem
	var metadata sql.NullString
	err := row.Scan(
		&item.ID, &item.Project, &item.ItemType, &item.ParentID,
		&item.Subject, &item.Description, &item.Status,
		&item.WorktreePath, &item.TicketID, &item.PRURL,
		&item.AgentID, &item.Priority, &metadata,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if metadata.Valid {
		item.Metadata = metadata.String
	}
	return &item, nil
}

func scanWorkItems(rows *sql.Rows) ([]*WorkItem, error) {
	var items []*WorkItem
	for rows.Next() {
		var item WorkItem
		var metadata sql.NullString
		err := rows.Scan(
			&item.ID, &item.Project, &item.ItemType, &item.ParentID,
			&item.Subject, &item.Description, &item.Status,
			&item.WorktreePath, &item.TicketID, &item.PRURL,
			&item.AgentID, &item.Priority, &metadata,
			&item.CreatedAt, &item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if metadata.Valid {
			item.Metadata = metadata.String
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}
