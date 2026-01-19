package store

import (
	"database/sql"
	"time"
)

// CreateBlackboardEntry inserts a new blackboard entry.
func (s *Store) CreateBlackboardEntry(entry *BlackboardEntry) error {
	// Auto-assign sequence number if not set
	if entry.Sequence == 0 {
		var maxSeq sql.NullInt64
		s.db.QueryRow(`SELECT MAX(sequence) FROM blackboard WHERE worktree_path = ?`, entry.WorktreePath).Scan(&maxSeq)
		if maxSeq.Valid {
			entry.Sequence = int(maxSeq.Int64) + 1
		} else {
			entry.Sequence = 1
		}
	}

	query := `INSERT INTO blackboard (id, worktree_path, entry_type, content, agent_id, sequence, created_at, resolved, resolved_by)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	_, err := s.db.Exec(query, entry.ID, entry.WorktreePath, entry.EntryType, entry.Content, entry.AgentID, entry.Sequence, now, entry.Resolved, entry.ResolvedBy)
	return err
}

// GetBlackboardEntry retrieves a blackboard entry by ID.
func (s *Store) GetBlackboardEntry(id string) (*BlackboardEntry, error) {
	query := `SELECT id, worktree_path, entry_type, content, agent_id, sequence, created_at, resolved, resolved_by
	          FROM blackboard WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var e BlackboardEntry
	err := row.Scan(&e.ID, &e.WorktreePath, &e.EntryType, &e.Content, &e.AgentID, &e.Sequence, &e.CreatedAt, &e.Resolved, &e.ResolvedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListBlackboardEntries retrieves all entries for a worktree, ordered by sequence.
func (s *Store) ListBlackboardEntries(worktreePath string) ([]*BlackboardEntry, error) {
	query := `SELECT id, worktree_path, entry_type, content, agent_id, sequence, created_at, resolved, resolved_by
	          FROM blackboard WHERE worktree_path = ? ORDER BY sequence ASC`
	rows, err := s.db.Query(query, worktreePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*BlackboardEntry
	for rows.Next() {
		var e BlackboardEntry
		if err := rows.Scan(&e.ID, &e.WorktreePath, &e.EntryType, &e.Content, &e.AgentID, &e.Sequence, &e.CreatedAt, &e.Resolved, &e.ResolvedBy); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// ListBlackboardEntriesByType retrieves entries for a worktree filtered by type.
func (s *Store) ListBlackboardEntriesByType(worktreePath string, entryType BlackboardEntryType) ([]*BlackboardEntry, error) {
	query := `SELECT id, worktree_path, entry_type, content, agent_id, sequence, created_at, resolved, resolved_by
	          FROM blackboard WHERE worktree_path = ? AND entry_type = ? ORDER BY sequence ASC`
	rows, err := s.db.Query(query, worktreePath, entryType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*BlackboardEntry
	for rows.Next() {
		var e BlackboardEntry
		if err := rows.Scan(&e.ID, &e.WorktreePath, &e.EntryType, &e.Content, &e.AgentID, &e.Sequence, &e.CreatedAt, &e.Resolved, &e.ResolvedBy); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// ListUnresolvedQuestions retrieves unresolved question entries for a worktree.
func (s *Store) ListUnresolvedQuestions(worktreePath string) ([]*BlackboardEntry, error) {
	query := `SELECT id, worktree_path, entry_type, content, agent_id, sequence, created_at, resolved, resolved_by
	          FROM blackboard WHERE worktree_path = ? AND entry_type = 'question' AND resolved = 0 ORDER BY sequence ASC`
	rows, err := s.db.Query(query, worktreePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*BlackboardEntry
	for rows.Next() {
		var e BlackboardEntry
		if err := rows.Scan(&e.ID, &e.WorktreePath, &e.EntryType, &e.Content, &e.AgentID, &e.Sequence, &e.CreatedAt, &e.Resolved, &e.ResolvedBy); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// ResolveBlackboardQuestion marks a question as resolved.
func (s *Store) ResolveBlackboardQuestion(id, resolvedBy string) error {
	query := `UPDATE blackboard SET resolved = 1, resolved_by = ? WHERE id = ?`
	_, err := s.db.Exec(query, resolvedBy, id)
	return err
}

// ClearBlackboard removes all entries for a worktree.
func (s *Store) ClearBlackboard(worktreePath string) error {
	query := `DELETE FROM blackboard WHERE worktree_path = ?`
	_, err := s.db.Exec(query, worktreePath)
	return err
}

// DeleteBlackboardEntry removes a single blackboard entry.
func (s *Store) DeleteBlackboardEntry(id string) error {
	query := `DELETE FROM blackboard WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// CountBlackboardEntries returns the count of entries by type for a worktree.
func (s *Store) CountBlackboardEntries(worktreePath string) (map[BlackboardEntryType]int, error) {
	query := `SELECT entry_type, COUNT(*) FROM blackboard WHERE worktree_path = ? GROUP BY entry_type`
	rows, err := s.db.Query(query, worktreePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[BlackboardEntryType]int)
	for rows.Next() {
		var entryType BlackboardEntryType
		var count int
		if err := rows.Scan(&entryType, &count); err != nil {
			return nil, err
		}
		counts[entryType] = count
	}
	return counts, rows.Err()
}

// CountUnresolvedQuestions returns the count of unresolved questions for a worktree.
func (s *Store) CountUnresolvedQuestions(worktreePath string) (int, error) {
	query := `SELECT COUNT(*) FROM blackboard
		WHERE worktree_path = ? AND entry_type = ? AND resolved = 0`
	var count int
	err := s.db.QueryRow(query, worktreePath, BlackboardTypeQuestion).Scan(&count)
	return count, err
}
