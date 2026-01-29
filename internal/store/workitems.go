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

// WorkItemFilter selects work items for listing.
type WorkItemFilter struct {
	Project  string
	ItemType string
	Status   string
}

// ListWorkItems retrieves work items matching the filter.
func (s *Store) ListWorkItems(filter WorkItemFilter) ([]*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
			worktree_path, ticket_id, pr_url, agent_id, priority, metadata, created_at, updated_at
		FROM work_items`

	var args []any
	var clauses []string
	if filter.Project != "" {
		clauses = append(clauses, "project = ?")
		args = append(args, filter.Project)
	}
	if filter.ItemType != "" {
		clauses = append(clauses, "item_type = ?")
		args = append(args, filter.ItemType)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY project, id"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*WorkItem
	for rows.Next() {
		item, err := scanWorkItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListWorkItemChildren returns the direct children of a work item.
func (s *Store) ListWorkItemChildren(parentID string) ([]*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
			worktree_path, ticket_id, pr_url, agent_id, priority, metadata, created_at, updated_at
		FROM work_items WHERE parent_id = ? ORDER BY id`

	rows, err := s.db.Query(query, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*WorkItem
	for rows.Next() {
		item, err := scanWorkItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetWorkItem retrieves a single work item by ID.
func (s *Store) GetWorkItem(id string) (*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
			worktree_path, ticket_id, pr_url, agent_id, priority, metadata, created_at, updated_at
		FROM work_items WHERE id = ?`
	row := s.db.QueryRow(query, id)
	item, err := scanWorkItemRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

// CreateWorkItem inserts a new work item and returns the stored record.
func (s *Store) CreateWorkItem(item *WorkItem) (*WorkItem, error) {
	if item == nil {
		return nil, fmt.Errorf("work item is nil")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if item.ID == "" {
		id, err := generateWorkItemID(tx, item.ParentID)
		if err != nil {
			return nil, err
		}
		item.ID = id
	}
	if item.Status == "" {
		item.Status = WorkItemStatusPending
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	item.UpdatedAt = time.Now()

	query := `
		INSERT INTO work_items (
			id, project, item_type, parent_id, subject, description, status,
			worktree_path, ticket_id, pr_url, agent_id, priority, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = tx.Exec(query,
		item.ID,
		item.Project,
		item.ItemType,
		item.ParentID,
		item.Subject,
		item.Description,
		item.Status,
		item.WorktreePath,
		item.TicketID,
		item.PRURL,
		item.AgentID,
		item.Priority,
		item.Metadata,
		item.CreatedAt,
		item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateWorkItem persists updates to a work item.
func (s *Store) UpdateWorkItem(item *WorkItem) error {
	if item == nil {
		return fmt.Errorf("work item is nil")
	}
	item.UpdatedAt = time.Now()
	query := `
		UPDATE work_items SET
			project = ?,
			item_type = ?,
			parent_id = ?,
			subject = ?,
			description = ?,
			status = ?,
			worktree_path = ?,
			ticket_id = ?,
			pr_url = ?,
			agent_id = ?,
			priority = ?,
			metadata = ?,
			updated_at = ?
		WHERE id = ?`

	_, err := s.db.Exec(query,
		item.Project,
		item.ItemType,
		item.ParentID,
		item.Subject,
		item.Description,
		item.Status,
		item.WorktreePath,
		item.TicketID,
		item.PRURL,
		item.AgentID,
		item.Priority,
		item.Metadata,
		item.UpdatedAt,
		item.ID,
	)
	return err
}

// DeleteWorkItem removes a work item by ID.
func (s *Store) DeleteWorkItem(id string) error {
	_, err := s.db.Exec(`DELETE FROM work_items WHERE id = ?`, id)
	return err
}

// ListReadyWorkItems returns pending tasks ready to work on.
func (s *Store) ListReadyWorkItems(project string) ([]*WorkItem, error) {
	query := `
		SELECT id, project, item_type, parent_id, subject, description, status,
			worktree_path, ticket_id, pr_url, agent_id, priority, metadata, created_at, updated_at
		FROM work_items WHERE status = 'pending' AND item_type = 'task'`
	var args []any
	if project != "" {
		query += " AND project = ?"
		args = append(args, project)
	}
	query += " ORDER BY priority, created_at"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*WorkItem
	for rows.Next() {
		item, err := scanWorkItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetWorkItemAncestors returns all ancestors of a work item, ordered from root to parent.
func (s *Store) GetWorkItemAncestors(id string) ([]*WorkItem, error) {
	item, err := s.GetWorkItem(id)
	if err != nil || item == nil {
		return nil, err
	}

	var ancestors []*WorkItem
	current := item
	for current.ParentID != nil && *current.ParentID != "" {
		parent, err := s.GetWorkItem(*current.ParentID)
		if err != nil || parent == nil {
			break
		}
		ancestors = append(ancestors, parent)
		current = parent
	}

	// reverse to root -> parent order
	for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
		ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
	}

	return ancestors, nil
}

func scanWorkItemRow(row *sql.Row) (*WorkItem, error) {
	var (
		parentID     sql.NullString
		description  sql.NullString
		worktreePath sql.NullString
		ticketID     sql.NullString
		prURL        sql.NullString
		agentID      sql.NullString
		metadata     sql.NullString
		itemType     string
		status       string
		priority     int
		createdAt    time.Time
		updatedAt    time.Time
	)

	item := &WorkItem{}
	err := row.Scan(
		&item.ID,
		&item.Project,
		&itemType,
		&parentID,
		&item.Subject,
		&description,
		&status,
		&worktreePath,
		&ticketID,
		&prURL,
		&agentID,
		&priority,
		&metadata,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	item.ItemType = WorkItemType(itemType)
	item.Status = WorkItemStatus(status)
	item.Priority = WorkItemPriority(priority)
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	if parentID.Valid {
		item.ParentID = &parentID.String
	}
	if description.Valid {
		item.Description = description.String
	}
	if worktreePath.Valid {
		item.WorktreePath = &worktreePath.String
	}
	if ticketID.Valid {
		item.TicketID = &ticketID.String
	}
	if prURL.Valid {
		item.PRURL = &prURL.String
	}
	if agentID.Valid {
		item.AgentID = &agentID.String
	}
	if metadata.Valid {
		item.Metadata = metadata.String
	}

	return item, nil
}

func scanWorkItemRows(rows *sql.Rows) (*WorkItem, error) {
	var (
		parentID     sql.NullString
		description  sql.NullString
		worktreePath sql.NullString
		ticketID     sql.NullString
		prURL        sql.NullString
		agentID      sql.NullString
		metadata     sql.NullString
		itemType     string
		status       string
		priority     int
		createdAt    time.Time
		updatedAt    time.Time
	)

	item := &WorkItem{}
	err := rows.Scan(
		&item.ID,
		&item.Project,
		&itemType,
		&parentID,
		&item.Subject,
		&description,
		&status,
		&worktreePath,
		&ticketID,
		&prURL,
		&agentID,
		&priority,
		&metadata,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	item.ItemType = WorkItemType(itemType)
	item.Status = WorkItemStatus(status)
	item.Priority = WorkItemPriority(priority)
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	if parentID.Valid {
		item.ParentID = &parentID.String
	}
	if description.Valid {
		item.Description = description.String
	}
	if worktreePath.Valid {
		item.WorktreePath = &worktreePath.String
	}
	if ticketID.Valid {
		item.TicketID = &ticketID.String
	}
	if prURL.Valid {
		item.PRURL = &prURL.String
	}
	if agentID.Valid {
		item.AgentID = &agentID.String
	}
	if metadata.Valid {
		item.Metadata = metadata.String
	}

	return item, nil
}

func generateWorkItemID(tx *sql.Tx, parentID *string) (string, error) {
	if parentID == nil || *parentID == "" {
		return generateRootWorkItemID(tx)
	}

	rows, err := tx.Query(`SELECT id FROM work_items WHERE parent_id = ?`, *parentID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	maxSuffix := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		idx := strings.LastIndex(id, ".")
		if idx == -1 || idx+1 >= len(id) {
			continue
		}
		suffix := id[idx+1:]
		if n, err := strconv.Atoi(suffix); err == nil && n > maxSuffix {
			maxSuffix = n
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%d", *parentID, maxSuffix+1), nil
}

func generateRootWorkItemID(tx *sql.Tx) (string, error) {
	for i := 0; i < 10; i++ {
		raw := make([]byte, 2)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		id := "wi-" + hex.EncodeToString(raw)
		var exists int
		err := tx.QueryRow(`SELECT 1 FROM work_items WHERE id = ? LIMIT 1`, id).Scan(&exists)
		if err == sql.ErrNoRows {
			return id, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("failed to generate work item id")
}
