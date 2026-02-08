package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CreateJob inserts a new job into the database.
func (s *Store) CreateJob(job *Job) error {
	query := `
		INSERT INTO jobs (
			id, raw_input, normalized_input, status, job_type, project, external_id, external_url,
			current_agent_id, agent_history, target_branch, worktree_path, commit_hash, answer,
			propagation_results, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	historyJSON, err := marshalJSONField(job.AgentHistory, "agent_history")
	if err != nil {
		return err
	}
	propagationJSON, err := marshalJSONField(job.PropagationResults, "propagation_results")
	if err != nil {
		return err
	}

	// Default job type to feature if not set
	jobType := job.Type
	if jobType == "" {
		jobType = JobTypeFeature
	}

	_, err = s.db.Exec(query,
		job.ID,
		job.RawInput,
		job.NormalizedInput,
		job.Status,
		jobType,
		job.Project,
		job.ExternalID,
		job.ExternalURL,
		job.CurrentAgentID,
		string(historyJSON),
		job.TargetBranch,
		job.WorktreePath,
		job.CommitHash,
		job.Answer,
		string(propagationJSON),
		now,
		now,
	)
	return err
}

// GetJob retrieves a job by ID.
func (s *Store) GetJob(id string) (*Job, error) {
	query := `
		SELECT id, raw_input, normalized_input, status, job_type, project, created_at, updated_at,
		       external_id, external_url, current_agent_id, agent_history,
		       target_branch, worktree_path, commit_hash, answer, propagation_results
		FROM jobs WHERE id = ?
	`
	row := s.db.QueryRow(query, id)
	return scanJob(row)
}

// ListJobs retrieves all jobs, optionally filtered by status.
func (s *Store) ListJobs(statusFilter ...JobStatus) ([]*Job, error) {
	var query string
	var args []any

	if len(statusFilter) > 0 {
		query = `
			SELECT id, raw_input, normalized_input, status, job_type, project, created_at, updated_at,
			       external_id, external_url, current_agent_id, agent_history,
			       target_branch, worktree_path, commit_hash, answer, propagation_results
			FROM jobs WHERE status IN (?` + repeatSQL(len(statusFilter)-1) + `)
			ORDER BY updated_at DESC
		`
		for _, s := range statusFilter {
			args = append(args, s)
		}
	} else {
		query = `
			SELECT id, raw_input, normalized_input, status, job_type, project, created_at, updated_at,
			       external_id, external_url, current_agent_id, agent_history,
			       target_branch, worktree_path, commit_hash, answer, propagation_results
			FROM jobs ORDER BY updated_at DESC
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ListPendingJobs returns all jobs with pending status.
func (s *Store) ListPendingJobs() ([]*Job, error) {
	return s.ListJobs(JobStatusPending)
}

// UpdateJobStatus updates a job's status.
func (s *Store) UpdateJobStatus(id string, status JobStatus) error {
	query := `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, status, time.Now(), id)
	return err
}

// UpdateJobAgent updates a job's current agent and adds to history.
func (s *Store) UpdateJobAgent(id string, agentID string) error {
	// First get current history
	job, err := s.GetJob(id)
	if err != nil {
		return err
	}
	if job == nil {
		return sql.ErrNoRows
	}

	// Add to history if not already the current agent
	history := job.AgentHistory
	if job.CurrentAgentID == nil || *job.CurrentAgentID != agentID {
		if job.CurrentAgentID != nil {
			history = append(history, *job.CurrentAgentID)
		}
	}
	historyJSON, err := marshalJSONField(history, "agent_history")
	if err != nil {
		return err
	}

	query := `UPDATE jobs SET current_agent_id = ?, agent_history = ?, updated_at = ? WHERE id = ?`
	_, err = s.db.Exec(query, agentID, historyJSON, time.Now(), id)
	return err
}

// DeleteJob removes a job from the database.
func (s *Store) DeleteJob(id string) error {
	query := `DELETE FROM jobs WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// UpdateJobAnswer sets the answer for a question job.
func (s *Store) UpdateJobAnswer(id string, answer string) error {
	query := `UPDATE jobs SET answer = ?, status = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, answer, JobStatusCompleted, time.Now(), id)
	return err
}

// UpdateJobWorktree sets the worktree path for a quick job.
func (s *Store) UpdateJobWorktree(id string, worktreePath string) error {
	query := `UPDATE jobs SET worktree_path = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, worktreePath, time.Now(), id)
	return err
}

// UpdateJobCommit sets the commit hash for a quick job.
func (s *Store) UpdateJobCommit(id string, commitHash string) error {
	query := `UPDATE jobs SET commit_hash = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, commitHash, time.Now(), id)
	return err
}

// UpdateJobPropagation updates the propagation results for a quick job.
func (s *Store) UpdateJobPropagation(id string, results []PropagationResult) error {
	resultsJSON, err := marshalJSONField(results, "propagation_results")
	if err != nil {
		return err
	}
	query := `UPDATE jobs SET propagation_results = ?, updated_at = ? WHERE id = ?`
	_, err = s.db.Exec(query, resultsJSON, time.Now(), id)
	return err
}

func scanJob(row *sql.Row) (*Job, error) {
	var j Job
	var historyJSON, propagationJSON sql.NullString
	err := row.Scan(
		&j.ID, &j.RawInput, &j.NormalizedInput, &j.Status, &j.Type, &j.Project, &j.CreatedAt, &j.UpdatedAt,
		&j.ExternalID, &j.ExternalURL, &j.CurrentAgentID, &historyJSON,
		&j.TargetBranch, &j.WorktreePath, &j.CommitHash, &j.Answer, &propagationJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalOptionalJSONField(historyJSON, "agent_history", &j.AgentHistory); err != nil {
		return nil, err
	}
	if err := unmarshalOptionalJSONField(propagationJSON, "propagation_results", &j.PropagationResults); err != nil {
		return nil, err
	}
	return &j, nil
}

func scanJobRows(rows *sql.Rows) (*Job, error) {
	var j Job
	var historyJSON, propagationJSON sql.NullString
	err := rows.Scan(
		&j.ID, &j.RawInput, &j.NormalizedInput, &j.Status, &j.Type, &j.Project, &j.CreatedAt, &j.UpdatedAt,
		&j.ExternalID, &j.ExternalURL, &j.CurrentAgentID, &historyJSON,
		&j.TargetBranch, &j.WorktreePath, &j.CommitHash, &j.Answer, &propagationJSON,
	)
	if err != nil {
		return nil, err
	}
	if err := unmarshalOptionalJSONField(historyJSON, "agent_history", &j.AgentHistory); err != nil {
		return nil, err
	}
	if err := unmarshalOptionalJSONField(propagationJSON, "propagation_results", &j.PropagationResults); err != nil {
		return nil, err
	}
	return &j, nil
}

func marshalJSONField(value any, field string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", field, err)
	}
	return string(encoded), nil
}

func unmarshalOptionalJSONField(raw sql.NullString, field string, out any) error {
	if !raw.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(raw.String)
	if trimmed == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(trimmed), out); err != nil {
		return fmt.Errorf("decode %s: %w", field, err)
	}
	return nil
}
