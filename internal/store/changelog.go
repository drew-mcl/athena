package store

import "time"

// CreateChangelogEntry inserts a new changelog entry.
func (s *Store) CreateChangelogEntry(entry *ChangelogEntry) error {
	query := `INSERT INTO changelog (id, title, description, category, project, job_id, agent_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query, entry.ID, entry.Title, entry.Description, entry.Category, entry.Project, entry.JobID, entry.AgentID, time.Now())
	return err
}

// ListChangelog retrieves changelog entries, optionally filtered by project.
func (s *Store) ListChangelog(project string, limit int) ([]*ChangelogEntry, error) {
	var query string
	var args []any

	if project != "" {
		query = `SELECT id, title, description, category, project, job_id, agent_id, created_at FROM changelog WHERE project = ? ORDER BY created_at DESC LIMIT ?`
		args = []any{project, limit}
	} else {
		query = `SELECT id, title, description, category, project, job_id, agent_id, created_at FROM changelog ORDER BY created_at DESC LIMIT ?`
		args = []any{limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*ChangelogEntry
	for rows.Next() {
		var e ChangelogEntry
		if err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Category, &e.Project, &e.JobID, &e.AgentID, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// GetChangelogEntry retrieves a changelog entry by ID.
func (s *Store) GetChangelogEntry(id string) (*ChangelogEntry, error) {
	query := `SELECT id, title, description, category, project, job_id, agent_id, created_at FROM changelog WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var e ChangelogEntry
	err := row.Scan(&e.ID, &e.Title, &e.Description, &e.Category, &e.Project, &e.JobID, &e.AgentID, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteChangelogEntry removes a changelog entry.
func (s *Store) DeleteChangelogEntry(id string) error {
	query := `DELETE FROM changelog WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}
