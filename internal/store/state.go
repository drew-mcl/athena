package store

import (
	"database/sql"
	"time"
)

// CreateStateEntry inserts a new project state entry.
func (s *Store) CreateStateEntry(entry *StateEntry) error {
	query := `INSERT INTO project_state (id, project, state_type, key, value, confidence, source_agent, source_ref, created_at, updated_at, superseded_by)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	_, err := s.db.Exec(query, entry.ID, entry.Project, entry.StateType, entry.Key, entry.Value, entry.Confidence, entry.SourceAgent, entry.SourceRef, now, now, entry.SupersededBy)
	return err
}

// GetStateEntry retrieves a state entry by ID.
func (s *Store) GetStateEntry(id string) (*StateEntry, error) {
	query := `SELECT id, project, state_type, key, value, confidence, source_agent, source_ref, created_at, updated_at, superseded_by
	          FROM project_state WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var e StateEntry
	err := row.Scan(&e.ID, &e.Project, &e.StateType, &e.Key, &e.Value, &e.Confidence, &e.SourceAgent, &e.SourceRef, &e.CreatedAt, &e.UpdatedAt, &e.SupersededBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetStateEntryByKey retrieves a state entry by project, type, and key.
func (s *Store) GetStateEntryByKey(project string, stateType StateEntryType, key string) (*StateEntry, error) {
	query := `SELECT id, project, state_type, key, value, confidence, source_agent, source_ref, created_at, updated_at, superseded_by
	          FROM project_state WHERE project = ? AND state_type = ? AND key = ?`
	row := s.db.QueryRow(query, project, stateType, key)

	var e StateEntry
	err := row.Scan(&e.ID, &e.Project, &e.StateType, &e.Key, &e.Value, &e.Confidence, &e.SourceAgent, &e.SourceRef, &e.CreatedAt, &e.UpdatedAt, &e.SupersededBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListStateEntries retrieves all active state entries for a project.
func (s *Store) ListStateEntries(project string) ([]*StateEntry, error) {
	query := `SELECT id, project, state_type, key, value, confidence, source_agent, source_ref, created_at, updated_at, superseded_by
	          FROM project_state WHERE project = ? AND superseded_by IS NULL ORDER BY state_type, key`
	rows, err := s.db.Query(query, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*StateEntry
	for rows.Next() {
		var e StateEntry
		if err := rows.Scan(&e.ID, &e.Project, &e.StateType, &e.Key, &e.Value, &e.Confidence, &e.SourceAgent, &e.SourceRef, &e.CreatedAt, &e.UpdatedAt, &e.SupersededBy); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// ListStateEntriesByType retrieves state entries for a project filtered by type.
func (s *Store) ListStateEntriesByType(project string, stateType StateEntryType) ([]*StateEntry, error) {
	query := `SELECT id, project, state_type, key, value, confidence, source_agent, source_ref, created_at, updated_at, superseded_by
	          FROM project_state WHERE project = ? AND state_type = ? AND superseded_by IS NULL ORDER BY key`
	rows, err := s.db.Query(query, project, stateType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*StateEntry
	for rows.Next() {
		var e StateEntry
		if err := rows.Scan(&e.ID, &e.Project, &e.StateType, &e.Key, &e.Value, &e.Confidence, &e.SourceAgent, &e.SourceRef, &e.CreatedAt, &e.UpdatedAt, &e.SupersededBy); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// UpsertStateEntry inserts or updates a state entry by project, type, and key.
// If an entry exists, it's superseded by the new one.
func (s *Store) UpsertStateEntry(entry *StateEntry) error {
	// Check for existing entry
	existing, err := s.GetStateEntryByKey(entry.Project, entry.StateType, entry.Key)
	if err != nil {
		return err
	}

	if existing != nil {
		// Mark existing as superseded
		_, err := s.db.Exec(`UPDATE project_state SET superseded_by = ? WHERE id = ?`, entry.ID, existing.ID)
		if err != nil {
			return err
		}
	}

	// Insert new entry
	return s.CreateStateEntry(entry)
}

// UpdateStateValue updates the value and confidence of a state entry.
func (s *Store) UpdateStateValue(id string, value string, confidence float64) error {
	query := `UPDATE project_state SET value = ?, confidence = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, value, confidence, time.Now(), id)
	return err
}

// DeleteStateEntry removes a state entry.
func (s *Store) DeleteStateEntry(id string) error {
	query := `DELETE FROM project_state WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// DeleteProjectState removes all state entries for a project.
func (s *Store) DeleteProjectState(project string) error {
	query := `DELETE FROM project_state WHERE project = ?`
	_, err := s.db.Exec(query, project)
	return err
}

// CountStateEntries returns the count of active entries by type for a project.
func (s *Store) CountStateEntries(project string) (map[StateEntryType]int, error) {
	query := `SELECT state_type, COUNT(*) FROM project_state WHERE project = ? AND superseded_by IS NULL GROUP BY state_type`
	rows, err := s.db.Query(query, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[StateEntryType]int)
	for rows.Next() {
		var stateType StateEntryType
		var count int
		if err := rows.Scan(&stateType, &count); err != nil {
			return nil, err
		}
		counts[stateType] = count
	}
	return counts, rows.Err()
}

// GetHighConfidenceState retrieves state entries above a confidence threshold.
func (s *Store) GetHighConfidenceState(project string, minConfidence float64) ([]*StateEntry, error) {
	query := `SELECT id, project, state_type, key, value, confidence, source_agent, source_ref, created_at, updated_at, superseded_by
	          FROM project_state WHERE project = ? AND superseded_by IS NULL AND confidence >= ? ORDER BY confidence DESC, state_type, key`
	rows, err := s.db.Query(query, project, minConfidence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*StateEntry
	for rows.Next() {
		var e StateEntry
		if err := rows.Scan(&e.ID, &e.Project, &e.StateType, &e.Key, &e.Value, &e.Confidence, &e.SourceAgent, &e.SourceRef, &e.CreatedAt, &e.UpdatedAt, &e.SupersededBy); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}
