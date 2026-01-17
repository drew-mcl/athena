package eventlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/drewfead/athena/internal/data"
	"github.com/drewfead/athena/internal/store"
	"github.com/google/uuid"
)

// SQLiteEventLog implements EventLog using the store package.
type SQLiteEventLog struct {
	store *store.Store
}

// NewSQLiteEventLog creates an event log backed by SQLite.
func NewSQLiteEventLog(s *store.Store) *SQLiteEventLog {
	return &SQLiteEventLog{store: s}
}

func (l *SQLiteEventLog) Append(ctx context.Context, agentID string, msg *data.Message) (int64, error) {
	// Get next sequence number
	count, err := l.store.CountMessages(agentID)
	if err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}

	seq := int64(count)
	msg.Sequence = seq

	// Ensure ID is set
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}

	// Ensure timestamp is set
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	if err := l.store.CreateMessage(msg); err != nil {
		return 0, fmt.Errorf("create message: %w", err)
	}

	return seq, nil
}

func (l *SQLiteEventLog) Read(ctx context.Context, agentID string, fromSeq int64, limit int) ([]*data.Message, error) {
	// Get all messages and filter by sequence
	// TODO: Add offset support to store.GetMessages for efficiency
	allMsgs, err := l.store.GetMessages(agentID, 10000)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	var result []*data.Message
	for _, msg := range allMsgs {
		if msg.Sequence >= fromSeq {
			result = append(result, msg)
			if len(result) >= limit {
				break
			}
		}
	}

	return result, nil
}

func (l *SQLiteEventLog) ReadAll(ctx context.Context, agentID string) ([]*data.Message, error) {
	msgs, err := l.store.GetMessages(agentID, 100000) // large limit
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return msgs, nil
}

func (l *SQLiteEventLog) LastSequence(ctx context.Context, agentID string) (int64, error) {
	count, err := l.store.CountMessages(agentID)
	if err != nil {
		return -1, fmt.Errorf("count messages: %w", err)
	}
	return int64(count - 1), nil
}

func (l *SQLiteEventLog) Snapshot(ctx context.Context, agentID string) (*Snapshot, error) {
	conv, err := l.store.GetConversation(agentID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	if len(conv.Messages) == 0 {
		return nil, fmt.Errorf("no messages for agent %s", agentID)
	}

	// Serialize conversation
	convData, err := json.Marshal(conv)
	if err != nil {
		return nil, fmt.Errorf("marshal conversation: %w", err)
	}

	// Compute checksum
	hash := sha256.Sum256(convData)
	checksum := hex.EncodeToString(hash[:])

	snap := &Snapshot{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Sequence:  int64(len(conv.Messages) - 1),
		Timestamp: time.Now(),
		Checksum:  checksum,
		Data:      convData,
		Metadata: SnapshotMeta{
			MessageCount:  len(conv.Messages),
			ToolCallCount: len(conv.ToolCalls()),
			Duration:      conv.Duration(),
			IsComplete:    conv.IsComplete(),
		},
	}

	// TODO: Persist snapshot to snapshots table
	return snap, nil
}

func (l *SQLiteEventLog) RestoreFromSnapshot(ctx context.Context, snap *Snapshot) error {
	// Verify checksum
	hash := sha256.Sum256(snap.Data)
	if hex.EncodeToString(hash[:]) != snap.Checksum {
		return fmt.Errorf("snapshot checksum mismatch")
	}

	// Deserialize conversation
	var conv data.Conversation
	if err := json.Unmarshal(snap.Data, &conv); err != nil {
		return fmt.Errorf("unmarshal conversation: %w", err)
	}

	// Clear existing messages
	if err := l.store.DeleteAgentMessages(snap.AgentID); err != nil {
		return fmt.Errorf("delete existing messages: %w", err)
	}

	// Re-insert messages from snapshot
	for _, msg := range conv.Messages {
		if err := l.store.CreateMessage(msg); err != nil {
			return fmt.Errorf("create message: %w", err)
		}
	}

	return nil
}

// =============================================================================
// Convenience: Create full pipeline with SQLite backend
// =============================================================================

// NewSQLitePipeline creates a Pipeline with SQLite event log, LRU cache, and simple bus.
func NewSQLitePipeline(s *store.Store, cacheSize int) *Pipeline {
	return NewPipeline(
		NewSQLiteEventLog(s),
		NewLRUContextCache(cacheSize),
		NewSimpleEventBus(),
	)
}
