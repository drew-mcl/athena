package store

import "time"

// CreatePlan inserts a new implementation plan.
func (s *Store) CreatePlan(plan *Plan) error {
	query := `INSERT INTO plans (id, worktree_path, agent_id, content, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	now := time.Now()
	_, err := s.db.Exec(query, plan.ID, plan.WorktreePath, plan.AgentID, plan.Content, plan.Status, now, now)
	return err
}

// GetPlan retrieves a plan by worktree path.
func (s *Store) GetPlan(worktreePath string) (*Plan, error) {
	query := `SELECT id, worktree_path, agent_id, content, status, created_at, updated_at FROM plans WHERE worktree_path = ? ORDER BY created_at DESC LIMIT 1`
	row := s.db.QueryRow(query, worktreePath)

	var p Plan
	err := row.Scan(&p.ID, &p.WorktreePath, &p.AgentID, &p.Content, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPlanByID retrieves a plan by its ID.
func (s *Store) GetPlanByID(id string) (*Plan, error) {
	query := `SELECT id, worktree_path, agent_id, content, status, created_at, updated_at FROM plans WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var p Plan
	err := row.Scan(&p.ID, &p.WorktreePath, &p.AgentID, &p.Content, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePlanStatus updates a plan's status.
func (s *Store) UpdatePlanStatus(worktreePath string, status PlanStatus) error {
	query := `UPDATE plans SET status = ?, updated_at = ? WHERE worktree_path = ?`
	_, err := s.db.Exec(query, status, time.Now(), worktreePath)
	return err
}

// UpdatePlanContent updates a plan's markdown content.
func (s *Store) UpdatePlanContent(worktreePath, content string) error {
	query := `UPDATE plans SET content = ?, updated_at = ? WHERE worktree_path = ?`
	_, err := s.db.Exec(query, content, time.Now(), worktreePath)
	return err
}

// DeletePlan removes a plan.
func (s *Store) DeletePlan(worktreePath string) error {
	query := `DELETE FROM plans WHERE worktree_path = ?`
	_, err := s.db.Exec(query, worktreePath)
	return err
}

// ListPlansByStatus retrieves plans with a specific status.
func (s *Store) ListPlansByStatus(status PlanStatus) ([]*Plan, error) {
	query := `SELECT id, worktree_path, agent_id, content, status, created_at, updated_at FROM plans WHERE status = ? ORDER BY updated_at DESC`
	rows, err := s.db.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*Plan
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.WorktreePath, &p.AgentID, &p.Content, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		plans = append(plans, &p)
	}
	return plans, rows.Err()
}
