package store

import (
	"database/sql"
	"time"
)

// UpsertWorktree inserts or updates a worktree.
func (s *Store) UpsertWorktree(wt *Worktree) error {
	// Default status to active if not set
	status := wt.Status
	if status == "" {
		status = WorktreeStatusActive
	}

	query := `
		INSERT INTO worktrees (path, project, branch, is_main, agent_id, job_id, discovered_at,
			ticket_id, ticket_hash, description, project_name, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			project = excluded.project,
			branch = excluded.branch,
			is_main = excluded.is_main,
			agent_id = COALESCE(excluded.agent_id, worktrees.agent_id),
			job_id = COALESCE(excluded.job_id, worktrees.job_id),
			ticket_id = COALESCE(excluded.ticket_id, worktrees.ticket_id),
			ticket_hash = COALESCE(excluded.ticket_hash, worktrees.ticket_hash),
			description = COALESCE(excluded.description, worktrees.description),
			project_name = COALESCE(excluded.project_name, worktrees.project_name),
			status = COALESCE(excluded.status, worktrees.status)
	`
	_, err := s.db.Exec(query,
		wt.Path,
		wt.Project,
		wt.Branch,
		wt.IsMain,
		wt.AgentID,
		wt.JobID,
		time.Now(),
		wt.TicketID,
		wt.TicketHash,
		wt.Description,
		wt.ProjectName,
		status,
	)
	return err
}

// GetWorktree retrieves a worktree by path.
func (s *Store) GetWorktree(path string) (*Worktree, error) {
	query := `
		SELECT path, project, branch, is_main, agent_id, job_id, discovered_at,
			ticket_id, ticket_hash, description, project_name, status
		FROM worktrees WHERE path = ?
	`
	row := s.db.QueryRow(query, path)
	return scanWorktree(row)
}

// ListWorktrees retrieves all worktrees, optionally filtered by project.
func (s *Store) ListWorktrees(project string) ([]*Worktree, error) {
	var query string
	var args []any

	if project != "" {
		query = `
			SELECT path, project, branch, is_main, agent_id, job_id, discovered_at,
				ticket_id, ticket_hash, description, project_name, status
			FROM worktrees WHERE project = ?
			ORDER BY is_main DESC, path
		`
		args = append(args, project)
	} else {
		query = `
			SELECT path, project, branch, is_main, agent_id, job_id, discovered_at,
				ticket_id, ticket_hash, description, project_name, status
			FROM worktrees ORDER BY project, is_main DESC, path
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var worktrees []*Worktree
	for rows.Next() {
		wt, err := scanWorktreeRows(rows)
		if err != nil {
			return nil, err
		}
		worktrees = append(worktrees, wt)
	}
	return worktrees, rows.Err()
}

// ListWorktreesWithAgents returns worktrees that have agents assigned.
func (s *Store) ListWorktreesWithAgents() ([]*Worktree, error) {
	query := `
		SELECT path, project, branch, is_main, agent_id, job_id, discovered_at,
			ticket_id, ticket_hash, description, project_name, status
		FROM worktrees WHERE agent_id IS NOT NULL
		ORDER BY project, path
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var worktrees []*Worktree
	for rows.Next() {
		wt, err := scanWorktreeRows(rows)
		if err != nil {
			return nil, err
		}
		worktrees = append(worktrees, wt)
	}
	return worktrees, rows.Err()
}

// AssignAgentToWorktree links an agent to a worktree.
func (s *Store) AssignAgentToWorktree(worktreePath, agentID string) error {
	query := `UPDATE worktrees SET agent_id = ? WHERE path = ?`
	_, err := s.db.Exec(query, agentID, worktreePath)
	return err
}

// ClearWorktreeAgent removes the agent assignment from a worktree.
func (s *Store) ClearWorktreeAgent(worktreePath string) error {
	query := `UPDATE worktrees SET agent_id = NULL WHERE path = ?`
	_, err := s.db.Exec(query, worktreePath)
	return err
}

// AssignJobToWorktree links a job to a worktree.
func (s *Store) AssignJobToWorktree(worktreePath, jobID string) error {
	query := `UPDATE worktrees SET job_id = ? WHERE path = ?`
	_, err := s.db.Exec(query, jobID, worktreePath)
	return err
}

// DeleteWorktree removes a worktree from the database.
func (s *Store) DeleteWorktree(path string) error {
	query := `DELETE FROM worktrees WHERE path = ?`
	_, err := s.db.Exec(query, path)
	return err
}

// UpdateWorktreeStatus sets the status of a worktree.
func (s *Store) UpdateWorktreeStatus(path string, status WorktreeStatus) error {
	query := `UPDATE worktrees SET status = ? WHERE path = ?`
	_, err := s.db.Exec(query, status, path)
	return err
}

// UpdateWorktreeTicket sets the ticket metadata for a worktree.
func (s *Store) UpdateWorktreeTicket(path, ticketID, ticketHash, description string) error {
	query := `UPDATE worktrees SET ticket_id = ?, ticket_hash = ?, description = ? WHERE path = ?`
	_, err := s.db.Exec(query, ticketID, ticketHash, description, path)
	return err
}

// UpdateWorktreeProjectName caches the project name from git remote.
func (s *Store) UpdateWorktreeProjectName(path, projectName string) error {
	query := `UPDATE worktrees SET project_name = ? WHERE path = ?`
	_, err := s.db.Exec(query, projectName, path)
	return err
}

// ListWorktreesByTicket retrieves worktrees for a specific ticket.
func (s *Store) ListWorktreesByTicket(ticketID string) ([]*Worktree, error) {
	query := `
		SELECT path, project, branch, is_main, agent_id, job_id, discovered_at,
			ticket_id, ticket_hash, description, project_name, status
		FROM worktrees WHERE ticket_id = ?
		ORDER BY path
	`
	rows, err := s.db.Query(query, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var worktrees []*Worktree
	for rows.Next() {
		wt, err := scanWorktreeRows(rows)
		if err != nil {
			return nil, err
		}
		worktrees = append(worktrees, wt)
	}
	return worktrees, rows.Err()
}

// ListWorktreesByStatus retrieves worktrees with a specific status.
func (s *Store) ListWorktreesByStatus(status WorktreeStatus) ([]*Worktree, error) {
	query := `
		SELECT path, project, branch, is_main, agent_id, job_id, discovered_at,
			ticket_id, ticket_hash, description, project_name, status
		FROM worktrees WHERE status = ?
		ORDER BY project, path
	`
	rows, err := s.db.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var worktrees []*Worktree
	for rows.Next() {
		wt, err := scanWorktreeRows(rows)
		if err != nil {
			return nil, err
		}
		worktrees = append(worktrees, wt)
	}
	return worktrees, rows.Err()
}

// UpdateWorktreePath updates the path of a worktree (used during normalize).
func (s *Store) UpdateWorktreePath(oldPath, newPath string) error {
	query := `UPDATE worktrees SET path = ? WHERE path = ?`
	_, err := s.db.Exec(query, newPath, oldPath)
	return err
}

// GetProjectNames returns all unique project names.
func (s *Store) GetProjectNames() ([]string, error) {
	query := `SELECT DISTINCT project FROM worktrees ORDER BY project`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func scanWorktree(row *sql.Row) (*Worktree, error) {
	var wt Worktree
	var status sql.NullString
	err := row.Scan(
		&wt.Path, &wt.Project, &wt.Branch, &wt.IsMain,
		&wt.AgentID, &wt.JobID, &wt.DiscoveredAt,
		&wt.TicketID, &wt.TicketHash, &wt.Description, &wt.ProjectName, &status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if status.Valid {
		wt.Status = WorktreeStatus(status.String)
	} else {
		wt.Status = WorktreeStatusActive
	}
	return &wt, err
}

func scanWorktreeRows(rows *sql.Rows) (*Worktree, error) {
	var wt Worktree
	var status sql.NullString
	err := rows.Scan(
		&wt.Path, &wt.Project, &wt.Branch, &wt.IsMain,
		&wt.AgentID, &wt.JobID, &wt.DiscoveredAt,
		&wt.TicketID, &wt.TicketHash, &wt.Description, &wt.ProjectName, &status,
	)
	if status.Valid {
		wt.Status = WorktreeStatus(status.String)
	} else {
		wt.Status = WorktreeStatusActive
	}
	return &wt, err
}
