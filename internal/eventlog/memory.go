package eventlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/data"
	"github.com/google/uuid"
)

// =============================================================================
// In-Memory EventLog (for testing / single-node)
// =============================================================================

// MemoryEventLog implements EventLog with in-memory storage.
type MemoryEventLog struct {
	mu        sync.RWMutex
	events    map[string][]*data.Message // agentID -> events
	snapshots map[string]*Snapshot       // agentID -> latest snapshot
}

// NewMemoryEventLog creates a new in-memory event log.
func NewMemoryEventLog() *MemoryEventLog {
	return &MemoryEventLog{
		events:    make(map[string][]*data.Message),
		snapshots: make(map[string]*Snapshot),
	}
}

func (l *MemoryEventLog) Append(ctx context.Context, agentID string, msg *data.Message) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	events := l.events[agentID]
	seq := int64(len(events))
	msg.Sequence = seq
	l.events[agentID] = append(events, msg)
	return seq, nil
}

func (l *MemoryEventLog) Read(ctx context.Context, agentID string, fromSeq int64, limit int) ([]*data.Message, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	events := l.events[agentID]
	if fromSeq >= int64(len(events)) {
		return nil, nil
	}

	end := int(fromSeq) + limit
	if end > len(events) {
		end = len(events)
	}

	return events[fromSeq:end], nil
}

func (l *MemoryEventLog) ReadAll(ctx context.Context, agentID string) ([]*data.Message, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.events[agentID], nil
}

func (l *MemoryEventLog) LastSequence(ctx context.Context, agentID string) (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return int64(len(l.events[agentID]) - 1), nil
}

func (l *MemoryEventLog) Snapshot(ctx context.Context, agentID string) (*Snapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	events := l.events[agentID]
	if len(events) == 0 {
		return nil, fmt.Errorf("no events for agent %s", agentID)
	}

	// Build conversation from events
	conv := data.NewConversation(agentID)
	for _, e := range events {
		conv.Messages = append(conv.Messages, e)
	}

	// Serialize
	convData, err := json.Marshal(conv)
	if err != nil {
		return nil, err
	}

	// Checksum
	hash := sha256.Sum256(convData)
	checksum := hex.EncodeToString(hash[:])

	snap := &Snapshot{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Sequence:  int64(len(events) - 1),
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

	l.snapshots[agentID] = snap
	return snap, nil
}

func (l *MemoryEventLog) RestoreFromSnapshot(ctx context.Context, snap *Snapshot) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Verify checksum
	hash := sha256.Sum256(snap.Data)
	if hex.EncodeToString(hash[:]) != snap.Checksum {
		return fmt.Errorf("snapshot checksum mismatch")
	}

	// Deserialize
	var conv data.Conversation
	if err := json.Unmarshal(snap.Data, &conv); err != nil {
		return err
	}

	// Restore events
	l.events[snap.AgentID] = conv.Messages
	l.snapshots[snap.AgentID] = snap
	return nil
}

// =============================================================================
// LRU Context Cache
// =============================================================================

// LRUContextCache implements ContextCache with LRU eviction.
type LRUContextCache struct {
	mu       sync.RWMutex
	cache    map[string]*lruEntry
	order    []string // LRU order (oldest first)
	maxSize  int
}

type lruEntry struct {
	ctx      *CachedContext
	lastUsed time.Time
}

// NewLRUContextCache creates a new LRU cache with the given max entries.
func NewLRUContextCache(maxSize int) *LRUContextCache {
	return &LRUContextCache{
		cache:   make(map[string]*lruEntry),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

func (c *LRUContextCache) Get(agentID string) (*CachedContext, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.cache[agentID]
	if !ok {
		return nil, false
	}

	// Update LRU order
	entry.lastUsed = time.Now()
	c.moveToEnd(agentID)

	return entry.ctx, true
}

func (c *LRUContextCache) Put(agentID string, ctx *CachedContext) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.cache[agentID]; ok {
		existing.ctx = ctx
		existing.lastUsed = time.Now()
		c.moveToEnd(agentID)
		return
	}

	// Evict if at capacity
	if len(c.cache) >= c.maxSize && c.maxSize > 0 {
		c.evictOldest()
	}

	c.cache[agentID] = &lruEntry{
		ctx:      ctx,
		lastUsed: time.Now(),
	}
	c.order = append(c.order, agentID)
}

func (c *LRUContextCache) Invalidate(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, agentID)
	c.removeFromOrder(agentID)
}

func (c *LRUContextCache) Warm(ctx context.Context, agentID string, log EventLog) error {
	msgs, err := log.ReadAll(ctx, agentID)
	if err != nil {
		return err
	}

	conv := data.NewConversation(agentID)
	for _, m := range msgs {
		conv.Messages = append(conv.Messages, m)
	}

	c.Put(agentID, &CachedContext{
		AgentID:      agentID,
		Conversation: conv,
		LastUpdated:  time.Now(),
	})

	return nil
}

func (c *LRUContextCache) moveToEnd(agentID string) {
	c.removeFromOrder(agentID)
	c.order = append(c.order, agentID)
}

func (c *LRUContextCache) removeFromOrder(agentID string) {
	for i, id := range c.order {
		if id == agentID {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *LRUContextCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.cache, oldest)
}

// =============================================================================
// Simple Event Bus
// =============================================================================

// SimpleEventBus implements EventBus with simple pub/sub.
type SimpleEventBus struct {
	mu           sync.RWMutex
	subscribers  map[string][]Subscriber // agentID -> subscribers
	globalSubs   []Subscriber            // subscribers to all events
}

// NewSimpleEventBus creates a new simple event bus.
func NewSimpleEventBus() *SimpleEventBus {
	return &SimpleEventBus{
		subscribers: make(map[string][]Subscriber),
	}
}

func (b *SimpleEventBus) Publish(msg *data.Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Agent-specific subscribers
	for _, sub := range b.subscribers[msg.AgentID] {
		go sub.OnEvent(msg) // Non-blocking
	}

	// Global subscribers
	for _, sub := range b.globalSubs {
		go sub.OnEvent(msg)
	}
}

func (b *SimpleEventBus) Subscribe(agentID string, sub Subscriber) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscribers[agentID] = append(b.subscribers[agentID], sub)

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[agentID]
		for i, s := range subs {
			if s == sub {
				b.subscribers[agentID] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

func (b *SimpleEventBus) SubscribeAll(sub Subscriber) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.globalSubs = append(b.globalSubs, sub)

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.globalSubs {
			if s == sub {
				b.globalSubs = append(b.globalSubs[:i], b.globalSubs[i+1:]...)
				return
			}
		}
	}
}

// =============================================================================
// Channel Subscriber (adapts channels to Subscriber interface)
// =============================================================================

// ChannelSubscriber sends events to a channel.
type ChannelSubscriber struct {
	events chan *data.Message
	errors chan error
}

// NewChannelSubscriber creates a subscriber that sends to channels.
func NewChannelSubscriber(bufSize int) *ChannelSubscriber {
	return &ChannelSubscriber{
		events: make(chan *data.Message, bufSize),
		errors: make(chan error, bufSize),
	}
}

func (s *ChannelSubscriber) OnEvent(msg *data.Message) {
	select {
	case s.events <- msg:
	default:
		// Buffer full, drop event (or could log)
	}
}

func (s *ChannelSubscriber) OnError(err error) {
	select {
	case s.errors <- err:
	default:
	}
}

// Events returns the event channel.
func (s *ChannelSubscriber) Events() <-chan *data.Message {
	return s.events
}

// Errors returns the error channel.
func (s *ChannelSubscriber) Errors() <-chan error {
	return s.errors
}

// Close closes the channels.
func (s *ChannelSubscriber) Close() {
	close(s.events)
	close(s.errors)
}
