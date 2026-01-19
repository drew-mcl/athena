package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Snapshot represents a conversation checkpoint for agent recovery.
type Snapshot struct {
	ID        string          `json:"id"`
	AgentID   string          `json:"agent_id"`
	Sequence  int64           `json:"sequence"` // Last event sequence included
	Timestamp time.Time       `json:"timestamp"`
	Checksum  string          `json:"checksum"` // For integrity verification
	Data      json.RawMessage `json:"data"`     // Serialized conversation state
	Metadata  SnapshotMeta    `json:"metadata"`
}

// SnapshotMeta contains metadata about the snapshot.
type SnapshotMeta struct {
	MessageCount  int           `json:"message_count"`
	ToolCallCount int           `json:"tool_call_count"`
	Duration      time.Duration `json:"duration"`
	IsComplete    bool          `json:"is_complete"`
}

// CreateSnapshot persists a snapshot to the database.
func (s *Store) CreateSnapshot(ctx context.Context, snap *Snapshot) error {
	query := `
		INSERT INTO snapshots (
			id, agent_id, sequence, timestamp, checksum, data,
			message_count, tool_call_count, duration_ms, is_complete, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Serialize metadata
	metadataJSON, _ := json.Marshal(snap.Metadata)

	_, err := s.db.ExecContext(ctx, query,
		snap.ID,
		snap.AgentID,
		snap.Sequence,
		snap.Timestamp,
		snap.Checksum,
		string(snap.Data),
		snap.Metadata.MessageCount,
		snap.Metadata.ToolCallCount,
		snap.Metadata.Duration.Milliseconds(),
		snap.Metadata.IsComplete,
		string(metadataJSON),
	)
	return err
}

// GetLatestSnapshot retrieves the most recent snapshot for an agent.
func (s *Store) GetLatestSnapshot(ctx context.Context, agentID string) (*Snapshot, error) {
	query := `
		SELECT id, agent_id, sequence, timestamp, checksum, data,
		       message_count, tool_call_count, duration_ms, is_complete, metadata
		FROM snapshots
		WHERE agent_id = ?
		ORDER BY sequence DESC
		LIMIT 1
	`
	row := s.db.QueryRowContext(ctx, query, agentID)
	return scanSnapshot(row)
}

// ListSnapshots retrieves all snapshots for an agent.
func (s *Store) ListSnapshots(ctx context.Context, agentID string) ([]*Snapshot, error) {
	query := `
		SELECT id, agent_id, sequence, timestamp, checksum, data,
		       message_count, tool_call_count, duration_ms, is_complete, metadata
		FROM snapshots
		WHERE agent_id = ?
		ORDER BY sequence DESC
	`
	rows, err := s.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

// DeleteSnapshot removes a snapshot from the database.
func (s *Store) DeleteSnapshot(ctx context.Context, id string) error {
	query := `DELETE FROM snapshots WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

// scanSnapshot scans a row into a Snapshot.
func scanSnapshot(scanner interface{ Scan(...interface{}) error }) (*Snapshot, error) {
	var snap Snapshot
	var (
		id, agentID, checksum, data             sql.NullString
		sequence                                sql.NullInt64
		timestamp                               sql.NullTime
		messageCount, toolCallCount, durationMs sql.NullInt64
		isComplete                              sql.NullBool
		metadata                                sql.NullString
	)

	err := scanner.Scan(
		&id, &agentID, &sequence, &timestamp, &checksum, &data,
		&messageCount, &toolCallCount, &durationMs, &isComplete, &metadata,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	snap.ID = id.String
	snap.AgentID = agentID.String
	snap.Sequence = sequence.Int64
	snap.Timestamp = timestamp.Time
	snap.Checksum = checksum.String
	snap.Data = []byte(data.String)

	// Parse metadata
	if metadata.Valid {
		json.Unmarshal([]byte(metadata.String), &snap.Metadata)
	}

	snap.Metadata.MessageCount = int(messageCount.Int64)
	snap.Metadata.ToolCallCount = int(toolCallCount.Int64)
	snap.Metadata.Duration = time.Duration(durationMs.Int64) * time.Millisecond
	snap.Metadata.IsComplete = isComplete.Bool

	return &snap, nil
}
