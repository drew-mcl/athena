package store

import (
	"database/sql"
	"time"
)

// CreateNote inserts a new note.
func (s *Store) CreateNote(note *Note) error {
	query := `INSERT INTO notes (id, content, done, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	now := time.Now()
	_, err := s.db.Exec(query, note.ID, note.Content, note.Done, now, now)
	return err
}

// ListNotes retrieves all notes ordered by creation time.
func (s *Store) ListNotes() ([]*Note, error) {
	query := `SELECT id, content, done, created_at, updated_at FROM notes ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []*Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Content, &n.Done, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, &n)
	}
	return notes, rows.Err()
}

// GetNote retrieves a note by ID.
func (s *Store) GetNote(id string) (*Note, error) {
	query := `SELECT id, content, done, created_at, updated_at FROM notes WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var n Note
	err := row.Scan(&n.ID, &n.Content, &n.Done, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// UpdateNoteDone toggles the done status of a note.
func (s *Store) UpdateNoteDone(id string, done bool) error {
	query := `UPDATE notes SET done = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, done, time.Now(), id)
	return err
}

// DeleteNote removes a note.
func (s *Store) DeleteNote(id string) error {
	query := `DELETE FROM notes WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}
