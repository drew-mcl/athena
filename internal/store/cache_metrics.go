package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// CreateContextCacheMetric creates a new context cache metric record.
func (s *Store) CreateContextCacheMetric(m *ContextCacheMetric) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}

	_, err := s.db.Exec(`
		INSERT INTO context_cache_metrics (
			id, agent_id, project_name, worktree_path,
			state_entries_count, state_tokens_estimate,
			blackboard_entries_count, blackboard_tokens_estimate,
			cache_reads_total, cache_reads_in_state, cache_reads_in_blackboard,
			total_context_tokens, cache_hit_rate, is_first_agent, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		m.ID, m.AgentID, m.ProjectName, m.WorktreePath,
		m.StateEntriesCount, m.StateTokensEstimate,
		m.BlackboardEntriesCount, m.BlackboardTokensEstimate,
		m.CacheReadsTotal, m.CacheReadsInState, m.CacheReadsInBlackboard,
		m.TotalContextTokens, m.CacheHitRate, m.IsFirstAgent, m.CreatedAt,
	)
	return err
}

// UpdateContextCacheMetric updates an existing context cache metric record.
func (s *Store) UpdateContextCacheMetric(m *ContextCacheMetric) error {
	_, err := s.db.Exec(`
		UPDATE context_cache_metrics SET
			cache_reads_total = ?,
			cache_reads_in_state = ?,
			cache_reads_in_blackboard = ?,
			cache_hit_rate = ?
		WHERE id = ?
	`,
		m.CacheReadsTotal, m.CacheReadsInState, m.CacheReadsInBlackboard,
		m.CacheHitRate, m.ID,
	)
	return err
}

// GetContextCacheMetricForAgent retrieves the cache metric for a specific agent.
func (s *Store) GetContextCacheMetricForAgent(agentID string) (*ContextCacheMetric, error) {
	m := &ContextCacheMetric{}
	var worktreePath sql.NullString

	err := s.db.QueryRow(`
		SELECT id, agent_id, project_name, worktree_path,
			state_entries_count, state_tokens_estimate,
			blackboard_entries_count, blackboard_tokens_estimate,
			cache_reads_total, cache_reads_in_state, cache_reads_in_blackboard,
			total_context_tokens, cache_hit_rate, is_first_agent, created_at
		FROM context_cache_metrics
		WHERE agent_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, agentID).Scan(
		&m.ID, &m.AgentID, &m.ProjectName, &worktreePath,
		&m.StateEntriesCount, &m.StateTokensEstimate,
		&m.BlackboardEntriesCount, &m.BlackboardTokensEstimate,
		&m.CacheReadsTotal, &m.CacheReadsInState, &m.CacheReadsInBlackboard,
		&m.TotalContextTokens, &m.CacheHitRate, &m.IsFirstAgent, &m.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if worktreePath.Valid {
		m.WorktreePath = worktreePath.String
	}

	return m, nil
}

// GetProjectCacheStats retrieves aggregated cache statistics for a project.
func (s *Store) GetProjectCacheStats(projectName string) (*ProjectCacheStats, error) {
	stats := &ProjectCacheStats{ProjectName: projectName}

	// Get counts and averages
	err := s.db.QueryRow(`
		SELECT
			COUNT(*) as total_agents,
			SUM(CASE WHEN is_first_agent THEN 1 ELSE 0 END) as first_agent_count,
			SUM(CASE WHEN NOT is_first_agent THEN 1 ELSE 0 END) as subsequent_agent_count,
			COALESCE(AVG(cache_hit_rate), 0) as avg_cache_hit_rate,
			COALESCE(SUM(state_tokens_estimate), 0) as total_state_tokens,
			COALESCE(SUM(blackboard_tokens_estimate), 0) as total_blackboard_tokens,
			COALESCE(SUM(cache_reads_total), 0) as total_cache_reads
		FROM context_cache_metrics
		WHERE project_name = ?
	`, projectName).Scan(
		&stats.TotalAgents,
		&stats.FirstAgentCount,
		&stats.SubsequentAgentCount,
		&stats.AvgCacheHitRate,
		&stats.TotalStateTokens,
		&stats.TotalBlackboardTokens,
		&stats.TotalCacheReads,
	)
	if err != nil {
		return nil, err
	}

	// Get average cache rate for first agents
	s.db.QueryRow(`
		SELECT COALESCE(AVG(cache_hit_rate), 0)
		FROM context_cache_metrics
		WHERE project_name = ? AND is_first_agent = TRUE
	`, projectName).Scan(&stats.AvgFirstAgentCacheRate)

	// Get average cache rate for subsequent agents
	s.db.QueryRow(`
		SELECT COALESCE(AVG(cache_hit_rate), 0)
		FROM context_cache_metrics
		WHERE project_name = ? AND is_first_agent = FALSE
	`, projectName).Scan(&stats.AvgSubsequentAgentCacheRate)

	return stats, nil
}

// IsFirstAgentOnProject checks if there are any previous agents recorded for a project.
// Returns true if no agents have been recorded yet (meaning the next agent would be the first).
func (s *Store) IsFirstAgentOnProject(projectName string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM context_cache_metrics WHERE project_name = ?
	`, projectName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// ListProjectCacheMetrics retrieves all cache metrics for a project, ordered by creation time.
func (s *Store) ListProjectCacheMetrics(projectName string, limit int) ([]ContextCacheMetric, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT id, agent_id, project_name, worktree_path,
			state_entries_count, state_tokens_estimate,
			blackboard_entries_count, blackboard_tokens_estimate,
			cache_reads_total, cache_reads_in_state, cache_reads_in_blackboard,
			total_context_tokens, cache_hit_rate, is_first_agent, created_at
		FROM context_cache_metrics
		WHERE project_name = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, projectName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []ContextCacheMetric
	for rows.Next() {
		var m ContextCacheMetric
		var worktreePath sql.NullString

		err := rows.Scan(
			&m.ID, &m.AgentID, &m.ProjectName, &worktreePath,
			&m.StateEntriesCount, &m.StateTokensEstimate,
			&m.BlackboardEntriesCount, &m.BlackboardTokensEstimate,
			&m.CacheReadsTotal, &m.CacheReadsInState, &m.CacheReadsInBlackboard,
			&m.TotalContextTokens, &m.CacheHitRate, &m.IsFirstAgent, &m.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if worktreePath.Valid {
			m.WorktreePath = worktreePath.String
		}

		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}
