package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// CreateMetrics creates a new metrics record for an agent session.
func (s *Store) CreateMetrics(agentID, sessionID string) (*AgentMetrics, error) {
	m := &AgentMetrics{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		SessionID: sessionID,
		StartedAt: time.Now(),
	}

	_, err := s.db.Exec(`
		INSERT INTO agent_metrics (id, agent_id, session_id, started_at)
		VALUES (?, ?, ?, ?)
	`, m.ID, m.AgentID, m.SessionID, m.StartedAt)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// UpdateMetrics updates the metrics record with usage data from a result event.
func (s *Store) UpdateMetrics(metricsID string, update MetricsUpdate) error {
	now := time.Now()
	_, err := s.db.Exec(`
		UPDATE agent_metrics SET
			input_tokens = ?,
			output_tokens = ?,
			cache_read_tokens = ?,
			cache_creation_tokens = ?,
			duration_ms = ?,
			api_time_ms = ?,
			cost_cents = ?,
			num_turns = ?,
			completed_at = ?
		WHERE id = ?
	`,
		update.InputTokens,
		update.OutputTokens,
		update.CacheReadTokens,
		update.CacheCreationTokens,
		update.DurationMS,
		update.APITimeMS,
		update.CostCents,
		update.NumTurns,
		now,
		metricsID,
	)
	return err
}

// MetricsUpdate contains fields to update on a metrics record.
type MetricsUpdate struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	DurationMS          int64
	APITimeMS           int64
	CostCents           int
	NumTurns            int
}

// IncrementToolCalls increments the tool call counter.
func (s *Store) IncrementToolCalls(metricsID string, success bool) error {
	query := `UPDATE agent_metrics SET tool_calls = tool_calls + 1`
	if success {
		query += `, tool_successes = tool_successes + 1`
	} else {
		query += `, tool_failures = tool_failures + 1`
	}
	query += ` WHERE id = ?`

	_, err := s.db.Exec(query, metricsID)
	return err
}

// IncrementToolSuccess increments the tool success counter.
func (s *Store) IncrementToolSuccess(metricsID string) error {
	_, err := s.db.Exec(`
		UPDATE agent_metrics SET tool_successes = tool_successes + 1
		WHERE id = ?
	`, metricsID)
	return err
}

// IncrementToolFailure increments the tool failure counter.
func (s *Store) IncrementToolFailure(metricsID string) error {
	_, err := s.db.Exec(`
		UPDATE agent_metrics SET tool_failures = tool_failures + 1
		WHERE id = ?
	`, metricsID)
	return err
}

// GetMetrics retrieves metrics for an agent session.
func (s *Store) GetMetrics(metricsID string) (*AgentMetrics, error) {
	m := &AgentMetrics{}
	var completedAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, agent_id, session_id,
			input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
			duration_ms, api_time_ms, cost_cents,
			num_turns, tool_calls, tool_successes, tool_failures,
			started_at, completed_at
		FROM agent_metrics WHERE id = ?
	`, metricsID).Scan(
		&m.ID, &m.AgentID, &m.SessionID,
		&m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheCreationTokens,
		&m.DurationMS, &m.APITimeMS, &m.CostCents,
		&m.NumTurns, &m.ToolCalls, &m.ToolSuccesses, &m.ToolFailures,
		&m.StartedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	if completedAt.Valid {
		m.CompletedAt = &completedAt.Time
	}

	return m, nil
}

// GetMetricsByAgent retrieves metrics for an agent by agent ID.
func (s *Store) GetMetricsByAgent(agentID string) (*AgentMetrics, error) {
	m := &AgentMetrics{}
	var completedAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, agent_id, session_id,
			input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
			duration_ms, api_time_ms, cost_cents,
			num_turns, tool_calls, tool_successes, tool_failures,
			started_at, completed_at
		FROM agent_metrics WHERE agent_id = ?
		ORDER BY started_at DESC LIMIT 1
	`, agentID).Scan(
		&m.ID, &m.AgentID, &m.SessionID,
		&m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheCreationTokens,
		&m.DurationMS, &m.APITimeMS, &m.CostCents,
		&m.NumTurns, &m.ToolCalls, &m.ToolSuccesses, &m.ToolFailures,
		&m.StartedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	if completedAt.Valid {
		m.CompletedAt = &completedAt.Time
	}

	return m, nil
}

// ListMetricsByProject returns aggregated metrics for all agents in a project.
func (s *Store) ListMetricsByProject(projectName string) ([]AgentMetrics, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.agent_id, m.session_id,
			m.input_tokens, m.output_tokens, m.cache_read_tokens, m.cache_creation_tokens,
			m.duration_ms, m.api_time_ms, m.cost_cents,
			m.num_turns, m.tool_calls, m.tool_successes, m.tool_failures,
			m.started_at, m.completed_at
		FROM agent_metrics m
		JOIN agents a ON m.agent_id = a.id
		WHERE a.project_name = ?
		ORDER BY m.started_at DESC
	`, projectName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []AgentMetrics
	for rows.Next() {
		var m AgentMetrics
		var completedAt sql.NullTime

		err := rows.Scan(
			&m.ID, &m.AgentID, &m.SessionID,
			&m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheCreationTokens,
			&m.DurationMS, &m.APITimeMS, &m.CostCents,
			&m.NumTurns, &m.ToolCalls, &m.ToolSuccesses, &m.ToolFailures,
			&m.StartedAt, &completedAt,
		)
		if err != nil {
			return nil, err
		}

		if completedAt.Valid {
			m.CompletedAt = &completedAt.Time
		}

		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}

// ProjectMetricsSummary contains aggregated metrics for a project.
type ProjectMetricsSummary struct {
	ProjectName          string
	TotalSessions        int
	TotalInputTokens     int
	TotalOutputTokens    int
	TotalCacheReadTokens int
	TotalCostCents       int
	TotalDurationMS      int64
	TotalAPITimeMS       int64
	TotalToolCalls       int
	TotalToolSuccesses   int
	TotalToolFailures    int
	AvgCacheHitRate      float64
}

// GetProjectMetricsSummary returns aggregated metrics for a project.
func (s *Store) GetProjectMetricsSummary(projectName string) (*ProjectMetricsSummary, error) {
	summary := &ProjectMetricsSummary{ProjectName: projectName}

	err := s.db.QueryRow(`
		SELECT
			COUNT(*) as total_sessions,
			COALESCE(SUM(m.input_tokens), 0) as total_input,
			COALESCE(SUM(m.output_tokens), 0) as total_output,
			COALESCE(SUM(m.cache_read_tokens), 0) as total_cache_read,
			COALESCE(SUM(m.cost_cents), 0) as total_cost,
			COALESCE(SUM(m.duration_ms), 0) as total_duration,
			COALESCE(SUM(m.api_time_ms), 0) as total_api_time,
			COALESCE(SUM(m.tool_calls), 0) as total_tools,
			COALESCE(SUM(m.tool_successes), 0) as total_tool_success,
			COALESCE(SUM(m.tool_failures), 0) as total_tool_failures
		FROM agent_metrics m
		JOIN agents a ON m.agent_id = a.id
		WHERE a.project_name = ?
	`, projectName).Scan(
		&summary.TotalSessions,
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalCacheReadTokens,
		&summary.TotalCostCents,
		&summary.TotalDurationMS,
		&summary.TotalAPITimeMS,
		&summary.TotalToolCalls,
		&summary.TotalToolSuccesses,
		&summary.TotalToolFailures,
	)
	if err != nil {
		return nil, err
	}

	// Calculate average cache hit rate
	totalInput := summary.TotalInputTokens + summary.TotalCacheReadTokens
	if totalInput > 0 {
		summary.AvgCacheHitRate = float64(summary.TotalCacheReadTokens) / float64(totalInput) * 100
	}

	return summary, nil
}

// MetricsTrend contains aggregate metrics for a time period.
type MetricsTrend struct {
	Period             string  // "today", "week", "all"
	TotalSessions      int
	AvgCacheHitRate    float64
	AvgTokensPerTask   int
	AvgCostCents       int
	AvgToolSuccessRate float64
	TotalCostCents     int
}

// GetMetricsTrend returns aggregate metrics for the specified period.
// Valid periods: "today", "week", "all"
func (s *Store) GetMetricsTrend(period string) (*MetricsTrend, error) {
	var dateFilter string
	switch period {
	case "today":
		dateFilter = "AND m.started_at >= date('now', 'start of day')"
	case "week":
		dateFilter = "AND m.started_at >= date('now', '-7 days')"
	case "all":
		dateFilter = ""
	default:
		dateFilter = "AND m.started_at >= date('now', '-7 days')" // Default to week
		period = "week"
	}

	query := `
		SELECT
			COUNT(*) as total_sessions,
			COALESCE(SUM(m.input_tokens + m.cache_read_tokens + m.cache_creation_tokens), 0) as total_tokens,
			COALESCE(SUM(m.cache_read_tokens), 0) as total_cache_read,
			COALESCE(SUM(m.cost_cents), 0) as total_cost,
			COALESCE(SUM(m.tool_calls), 0) as total_tool_calls,
			COALESCE(SUM(m.tool_successes), 0) as total_tool_successes
		FROM agent_metrics m
		WHERE 1=1 ` + dateFilter

	var totalSessions int
	var totalTokens int
	var totalCacheRead int
	var totalCost int
	var totalToolCalls int
	var totalToolSuccesses int

	err := s.db.QueryRow(query).Scan(
		&totalSessions,
		&totalTokens,
		&totalCacheRead,
		&totalCost,
		&totalToolCalls,
		&totalToolSuccesses,
	)
	if err != nil {
		return nil, err
	}

	trend := &MetricsTrend{
		Period:         period,
		TotalSessions:  totalSessions,
		TotalCostCents: totalCost,
	}

	// Calculate averages
	if totalSessions > 0 {
		trend.AvgTokensPerTask = totalTokens / totalSessions
		trend.AvgCostCents = totalCost / totalSessions
	}

	// Calculate cache hit rate
	if totalTokens > 0 {
		trend.AvgCacheHitRate = float64(totalCacheRead) / float64(totalTokens) * 100
	}

	// Calculate tool success rate
	if totalToolCalls > 0 {
		trend.AvgToolSuccessRate = float64(totalToolSuccesses) / float64(totalToolCalls) * 100
	}

	return trend, nil
}

// FileAccess represents a record of file access by an agent.
type FileAccess struct {
	ID             string
	AgentID        string
	SessionID      string
	FilePath       string
	AccessCount    int
	TokensConsumed int
	FirstAccessAt  time.Time
	LastAccessAt   time.Time
}

// RecordFileAccess records or updates a file access entry for an agent.
// If the agent has already accessed this file, it increments the access count and adds tokens.
func (s *Store) RecordFileAccess(agentID, sessionID, filePath string, tokens int) error {
	now := time.Now()
	id := uuid.NewString()

	// Use INSERT OR REPLACE with conflict resolution to handle upsert
	_, err := s.db.Exec(`
		INSERT INTO file_access (id, agent_id, session_id, file_path, access_count, tokens_consumed, first_access_at, last_access_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT(agent_id, file_path) DO UPDATE SET
			access_count = access_count + 1,
			tokens_consumed = tokens_consumed + excluded.tokens_consumed,
			last_access_at = excluded.last_access_at
	`, id, agentID, sessionID, filePath, tokens, now, now)
	return err
}

// GetFileAccessByAgent retrieves all file access records for an agent.
func (s *Store) GetFileAccessByAgent(agentID string) ([]FileAccess, error) {
	rows, err := s.db.Query(`
		SELECT id, agent_id, session_id, file_path, access_count, tokens_consumed, first_access_at, last_access_at
		FROM file_access
		WHERE agent_id = ?
		ORDER BY last_access_at DESC
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accesses []FileAccess
	for rows.Next() {
		var fa FileAccess
		err := rows.Scan(
			&fa.ID, &fa.AgentID, &fa.SessionID, &fa.FilePath,
			&fa.AccessCount, &fa.TokensConsumed, &fa.FirstAccessAt, &fa.LastAccessAt,
		)
		if err != nil {
			return nil, err
		}
		accesses = append(accesses, fa)
	}

	return accesses, rows.Err()
}

// GetTopFilesByTokens retrieves the most expensive files (by token consumption) for an agent.
func (s *Store) GetTopFilesByTokens(agentID string, limit int) ([]FileAccess, error) {
	rows, err := s.db.Query(`
		SELECT id, agent_id, session_id, file_path, access_count, tokens_consumed, first_access_at, last_access_at
		FROM file_access
		WHERE agent_id = ?
		ORDER BY tokens_consumed DESC
		LIMIT ?
	`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accesses []FileAccess
	for rows.Next() {
		var fa FileAccess
		err := rows.Scan(
			&fa.ID, &fa.AgentID, &fa.SessionID, &fa.FilePath,
			&fa.AccessCount, &fa.TokensConsumed, &fa.FirstAccessAt, &fa.LastAccessAt,
		)
		if err != nil {
			return nil, err
		}
		accesses = append(accesses, fa)
	}

	return accesses, rows.Err()
}

// GetFileAccessStats returns summary statistics for an agent's file access.
func (s *Store) GetFileAccessStats(agentID string) (totalFiles int, totalAccesses int, avgAccessCount float64, err error) {
	err = s.db.QueryRow(`
		SELECT
			COUNT(*) as total_files,
			COALESCE(SUM(access_count), 0) as total_accesses,
			COALESCE(AVG(access_count), 0) as avg_access_count
		FROM file_access
		WHERE agent_id = ?
	`, agentID).Scan(&totalFiles, &totalAccesses, &avgAccessCount)
	return
}
