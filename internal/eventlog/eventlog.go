// Package eventlog provides event sourcing for agent conversations.
//
// Architecture:
//   - EventLog: Append-only event storage with snapshots
//   - ContextCache: In-memory LRU cache for fast context retrieval
//   - EventBus: Pub/sub for real-time streaming to TUI/monitors
//   - Replicator: Async replication to external storage (MySQL/Postgres)
//
// Events flow: Runner → EventLog → EventBus → Subscribers
//                          ↓
//                    ContextCache
//                          ↓
//                     Replicator → External DB
package eventlog

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/data"
)

// =============================================================================
// Core Interfaces
// =============================================================================

// EventLog is the append-only event store (source of truth).
type EventLog interface {
	// Append adds an event to the log. Returns the assigned sequence number.
	Append(ctx context.Context, agentID string, msg *data.Message) (int64, error)

	// Read retrieves events for an agent from a starting sequence.
	Read(ctx context.Context, agentID string, fromSeq int64, limit int) ([]*data.Message, error)

	// ReadAll retrieves all events for an agent.
	ReadAll(ctx context.Context, agentID string) ([]*data.Message, error)

	// LastSequence returns the last sequence number for an agent.
	LastSequence(ctx context.Context, agentID string) (int64, error)

	// Snapshot creates a checkpoint for fast restoration.
	Snapshot(ctx context.Context, agentID string) (*Snapshot, error)

	// RestoreFromSnapshot loads state from a snapshot.
	RestoreFromSnapshot(ctx context.Context, snap *Snapshot) error
}

// Snapshot represents a point-in-time checkpoint.
type Snapshot struct {
	ID        string          `json:"id"`
	AgentID   string          `json:"agent_id"`
	Sequence  int64           `json:"sequence"`    // Last event sequence included
	Timestamp time.Time       `json:"timestamp"`
	Checksum  string          `json:"checksum"`    // For integrity verification
	Data      json.RawMessage `json:"data"`        // Serialized conversation state
	Metadata  SnapshotMeta    `json:"metadata"`
}

// SnapshotMeta contains metadata about the snapshot.
type SnapshotMeta struct {
	MessageCount  int           `json:"message_count"`
	ToolCallCount int           `json:"tool_call_count"`
	Duration      time.Duration `json:"duration"`
	IsComplete    bool          `json:"is_complete"`
}

// =============================================================================
// Context Cache
// =============================================================================

// ContextCache provides fast in-memory access to conversation context.
type ContextCache interface {
	// Get retrieves cached context for an agent.
	Get(agentID string) (*CachedContext, bool)

	// Put stores context in cache.
	Put(agentID string, ctx *CachedContext)

	// Invalidate removes context from cache.
	Invalidate(agentID string)

	// Warm pre-loads context for an agent from the event log.
	Warm(ctx context.Context, agentID string, log EventLog) error
}

// CachedContext holds the in-memory conversation state.
type CachedContext struct {
	AgentID      string
	Conversation *data.Conversation
	LastUpdated  time.Time
	Snapshot     *Snapshot // nil if built from events
}

// =============================================================================
// Event Bus (Pub/Sub)
// =============================================================================

// Subscriber receives events from the bus.
type Subscriber interface {
	// OnEvent is called for each event. Should be non-blocking.
	OnEvent(msg *data.Message)

	// OnError is called when an error occurs.
	OnError(err error)
}

// EventBus distributes events to subscribers.
type EventBus interface {
	// Publish sends an event to all subscribers.
	Publish(msg *data.Message)

	// Subscribe registers a subscriber for an agent's events.
	// Returns an unsubscribe function.
	Subscribe(agentID string, sub Subscriber) func()

	// SubscribeAll registers a subscriber for all events.
	SubscribeAll(sub Subscriber) func()
}

// =============================================================================
// Replicator (Async External Sync)
// =============================================================================

// ReplicatorTarget is the destination for replicated events.
type ReplicatorTarget interface {
	// Write persists events to external storage.
	Write(ctx context.Context, msgs []*data.Message) error

	// WriteSnapshot persists a snapshot to external storage.
	WriteSnapshot(ctx context.Context, snap *Snapshot) error

	// LastReplicated returns the last sequence replicated for an agent.
	LastReplicated(ctx context.Context, agentID string) (int64, error)
}

// Replicator handles async replication to external storage.
type Replicator interface {
	// Start begins the replication loop.
	Start(ctx context.Context) error

	// Stop gracefully stops replication.
	Stop() error

	// Flush forces immediate replication of pending events.
	Flush(ctx context.Context) error

	// Status returns replication lag and health.
	Status() ReplicatorStatus
}

// ReplicatorStatus reports replication health.
type ReplicatorStatus struct {
	Healthy         bool
	PendingEvents   int
	LastReplicated  time.Time
	LagDuration     time.Duration
	ErrorCount      int
	LastError       error
	LastErrorTime   *time.Time
}

// =============================================================================
// Pipeline (combines all layers)
// =============================================================================

// Pipeline orchestrates the event flow through all layers.
type Pipeline struct {
	Log       EventLog
	Cache     ContextCache
	Bus       EventBus
	Replicator Replicator // optional

	mu sync.RWMutex
}

// NewPipeline creates a new event pipeline.
func NewPipeline(log EventLog, cache ContextCache, bus EventBus) *Pipeline {
	return &Pipeline{
		Log:   log,
		Cache: cache,
		Bus:   bus,
	}
}

// WithReplicator adds async replication to the pipeline.
func (p *Pipeline) WithReplicator(r Replicator) *Pipeline {
	p.Replicator = r
	return p
}

// Ingest processes an incoming event through all layers.
func (p *Pipeline) Ingest(ctx context.Context, msg *data.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Append to event log (source of truth)
	seq, err := p.Log.Append(ctx, msg.AgentID, msg)
	if err != nil {
		return err
	}
	msg.Sequence = seq

	// 2. Update cache
	cached, ok := p.Cache.Get(msg.AgentID)
	if ok {
		cached.Conversation.Append(msg)
		cached.LastUpdated = time.Now()
		p.Cache.Put(msg.AgentID, cached)
	}

	// 3. Publish to bus (non-blocking)
	p.Bus.Publish(msg)

	// Replicator picks up from log asynchronously
	return nil
}

// GetContext retrieves conversation context, using cache if available.
func (p *Pipeline) GetContext(ctx context.Context, agentID string) (*data.Conversation, error) {
	// Try cache first
	if cached, ok := p.Cache.Get(agentID); ok {
		return cached.Conversation, nil
	}

	// Cache miss - load from event log
	msgs, err := p.Log.ReadAll(ctx, agentID)
	if err != nil {
		return nil, err
	}

	conv := data.NewConversation(agentID)
	for _, m := range msgs {
		conv.Messages = append(conv.Messages, m)
	}

	// Populate cache
	p.Cache.Put(agentID, &CachedContext{
		AgentID:      agentID,
		Conversation: conv,
		LastUpdated:  time.Now(),
	})

	return conv, nil
}

// CreateSnapshot creates a snapshot for an agent.
func (p *Pipeline) CreateSnapshot(ctx context.Context, agentID string) (*Snapshot, error) {
	return p.Log.Snapshot(ctx, agentID)
}

// RestoreAgent restores an agent's context from snapshot or events.
func (p *Pipeline) RestoreAgent(ctx context.Context, agentID string, snap *Snapshot) (*data.Conversation, error) {
	if snap != nil {
		if err := p.Log.RestoreFromSnapshot(ctx, snap); err != nil {
			return nil, err
		}
	}

	// Warm the cache
	if err := p.Cache.Warm(ctx, agentID, p.Log); err != nil {
		return nil, err
	}

	return p.GetContext(ctx, agentID)
}
