package store

import (
	"database/sql"
	"time"
)

// CreateAgent inserts a new agent into the database.
func (s *Store) CreateAgent(agent *Agent) error {
	query := `
		INSERT INTO agents (
			id, worktree_path, project_name, archetype, status, pid, prompt,
			linear_issue_id, parent_agent_id, claude_session_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	_, err := s.db.Exec(query,
		agent.ID,
		agent.WorktreePath,
		agent.ProjectName,
		agent.Archetype,
		agent.Status,
		agent.PID,
		agent.Prompt,
		agent.LinearIssueID,
		agent.ParentAgentID,
		agent.ClaudeSessionID,
		now,
		now,
	)
	return err
}

// GetAgent retrieves an agent by ID.
func (s *Store) GetAgent(id string) (*Agent, error) {
	query := `
		SELECT id, worktree_path, project_name, archetype, status, pid, exit_code,
		       restart_count, created_at, updated_at, prompt, linear_issue_id,
		       parent_agent_id, claude_session_id, last_heartbeat
		FROM agents WHERE id = ?
	`
	row := s.db.QueryRow(query, id)
	return scanAgent(row)
}

// GetAgentBySessionID retrieves an agent by Claude session ID.
func (s *Store) GetAgentBySessionID(sessionID string) (*Agent, error) {
	query := `
		SELECT id, worktree_path, project_name, archetype, status, pid, exit_code,
		       restart_count, created_at, updated_at, prompt, linear_issue_id,
		       parent_agent_id, claude_session_id, last_heartbeat
		FROM agents WHERE claude_session_id = ?
	`
	row := s.db.QueryRow(query, sessionID)
	return scanAgent(row)
}

// ListAgents retrieves all agents, optionally filtered by status.
func (s *Store) ListAgents(statusFilter ...AgentStatus) ([]*Agent, error) {
	var query string
	var args []interface{}

	if len(statusFilter) > 0 {
		query = `
			SELECT id, worktree_path, project_name, archetype, status, pid, exit_code,
			       restart_count, created_at, updated_at, prompt, linear_issue_id,
			       parent_agent_id, claude_session_id, last_heartbeat
			FROM agents WHERE status IN (?` + repeatSQL(len(statusFilter)-1) + `)
			ORDER BY updated_at DESC
		`
		for _, s := range statusFilter {
			args = append(args, s)
		}
	} else {
		query = `
			SELECT id, worktree_path, project_name, archetype, status, pid, exit_code,
			       restart_count, created_at, updated_at, prompt, linear_issue_id,
			       parent_agent_id, claude_session_id, last_heartbeat
			FROM agents ORDER BY updated_at DESC
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		agent, err := scanAgentRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// ListRunningAgents returns all agents with running-like status.
func (s *Store) ListRunningAgents() ([]*Agent, error) {
	return s.ListAgents(
		AgentStatusRunning,
		AgentStatusPlanning,
		AgentStatusExecuting,
		AgentStatusSpawning,
		AgentStatusAwaiting,
	)
}

// ListAgentsByWorktree returns all agents for a specific worktree.
func (s *Store) ListAgentsByWorktree(worktreePath string) ([]*Agent, error) {
	query := `
		SELECT id, worktree_path, project_name, archetype, status, pid, exit_code,
		       restart_count, created_at, updated_at, prompt, linear_issue_id,
		       parent_agent_id, claude_session_id, last_heartbeat
		FROM agents WHERE worktree_path = ?
		ORDER BY created_at DESC
	`
	rows, err := s.db.Query(query, worktreePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		agent, err := scanAgentRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// UpdateAgentStatus updates an agent's status.
func (s *Store) UpdateAgentStatus(id string, status AgentStatus) error {
	query := `UPDATE agents SET status = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, status, time.Now(), id)
	return err
}

// UpdateAgentPID updates an agent's process ID.
func (s *Store) UpdateAgentPID(id string, pid int) error {
	query := `UPDATE agents SET pid = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, pid, time.Now(), id)
	return err
}

// UpdateAgentSessionID updates an agent's session ID.
func (s *Store) UpdateAgentSessionID(id, sessionID string) error {
	query := `UPDATE agents SET session_id = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, sessionID, time.Now(), id)
	return err
}

// UpdateAgentClaudeSessionID updates an agent's Claude session ID.
func (s *Store) UpdateAgentClaudeSessionID(id, sessionID string) error {
	query := `UPDATE agents SET claude_session_id = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, sessionID, time.Now(), id)
	return err
}

// UpdateAgentExitCode updates an agent's exit code.
func (s *Store) UpdateAgentExitCode(id string, exitCode int) error {
	query := `UPDATE agents SET exit_code = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, exitCode, time.Now(), id)
	return err
}

// IncrementRestartCount increments an agent's restart counter.
func (s *Store) IncrementRestartCount(id string) error {
	query := `UPDATE agents SET restart_count = restart_count + 1, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, time.Now(), id)
	return err
}

// UpdateHeartbeat updates an agent's last heartbeat time.
func (s *Store) UpdateHeartbeat(id string) error {
	query := `UPDATE agents SET last_heartbeat = ?, updated_at = ? WHERE id = ?`
	now := time.Now()
	_, err := s.db.Exec(query, now, now, id)
	return err
}

// DeleteAgent removes an agent from the database.
func (s *Store) DeleteAgent(id string) error {
	query := `DELETE FROM agents WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// DeleteAgentCascade removes an agent and all dependent records in correct order.
// This handles foreign key constraints by deleting dependents first:
// 1. Plans referencing this agent
// 2. Messages referencing this agent
// 3. Agent events referencing this agent
// 4. Clear agent_id from worktrees
// 5. Clear current_agent_id from jobs
// 6. Clear agent_id from changelog
// 7. Handle parent_agent_id self-reference (set to NULL)
// 8. Finally delete the agent
func (s *Store) DeleteAgentCascade(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete plans referencing this agent
	if _, err := tx.Exec(`DELETE FROM plans WHERE agent_id = ?`, id); err != nil {
		return err
	}

	// 2. Delete messages referencing this agent
	if _, err := tx.Exec(`DELETE FROM messages WHERE agent_id = ?`, id); err != nil {
		return err
	}

	// 3. Delete agent events referencing this agent
	if _, err := tx.Exec(`DELETE FROM agent_events WHERE agent_id = ?`, id); err != nil {
		return err
	}

	// 4. Clear agent_id from worktrees
	if _, err := tx.Exec(`UPDATE worktrees SET agent_id = NULL WHERE agent_id = ?`, id); err != nil {
		return err
	}

	// 5. Clear current_agent_id from jobs
	if _, err := tx.Exec(`UPDATE jobs SET current_agent_id = NULL WHERE current_agent_id = ?`, id); err != nil {
		return err
	}

	// 6. Clear agent_id from changelog
	if _, err := tx.Exec(`UPDATE changelog SET agent_id = NULL WHERE agent_id = ?`, id); err != nil {
		return err
	}

	// 7. Handle parent_agent_id self-reference (set to NULL for any children)
	if _, err := tx.Exec(`UPDATE agents SET parent_agent_id = NULL WHERE parent_agent_id = ?`, id); err != nil {
		return err
	}

	// 8. Finally delete the agent
	if _, err := tx.Exec(`DELETE FROM agents WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
}

// LogAgentEvent stores an event for an agent.
func (s *Store) LogAgentEvent(agentID, eventType, payload string) error {
	query := `INSERT INTO agent_events (agent_id, event_type, payload) VALUES (?, ?, ?)`
	_, err := s.db.Exec(query, agentID, eventType, payload)
	return err
}

// GetAgentEvents retrieves events for an agent.
func (s *Store) GetAgentEvents(agentID string, limit int) ([]*AgentEvent, error) {
	query := `
		SELECT id, agent_id, event_type, payload, timestamp
		FROM agent_events
		WHERE agent_id = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*AgentEvent
	for rows.Next() {
		var e AgentEvent
		if err := rows.Scan(&e.ID, &e.AgentID, &e.EventType, &e.Payload, &e.Timestamp); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}

func scanAgent(row *sql.Row) (*Agent, error) {
	var a Agent
	err := row.Scan(
		&a.ID, &a.WorktreePath, &a.ProjectName, &a.Archetype, &a.Status, &a.PID,
		&a.ExitCode, &a.RestartCount, &a.CreatedAt, &a.UpdatedAt, &a.Prompt,
		&a.LinearIssueID, &a.ParentAgentID, &a.ClaudeSessionID, &a.LastHeartbeat,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

func scanAgentRows(rows *sql.Rows) (*Agent, error) {
	var a Agent
	err := rows.Scan(
		&a.ID, &a.WorktreePath, &a.ProjectName, &a.Archetype, &a.Status, &a.PID,
		&a.ExitCode, &a.RestartCount, &a.CreatedAt, &a.UpdatedAt, &a.Prompt,
		&a.LinearIssueID, &a.ParentAgentID, &a.ClaudeSessionID, &a.LastHeartbeat,
	)
	return &a, err
}

func repeatSQL(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += ", ?"
	}
	return s
}
