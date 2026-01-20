// Package control provides stream types for real-time data flow visualization.
package control

import (
	"encoding/json"
	"time"
)

// StreamEvent represents a real-time event in the data flow.
// These are richer than the existing broadcast Event type, designed
// specifically for visualization and observability.
type StreamEvent struct {
	// ID is a unique identifier for this event
	ID string `json:"id"`
	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`
	// Type categorizes the event (see StreamEventType constants)
	Type StreamEventType `json:"type"`
	// Source identifies where the event originated
	Source StreamSource `json:"source"`
	// AgentID is set for agent-related events
	AgentID string `json:"agent_id,omitempty"`
	// WorktreePath is set for worktree-scoped events
	WorktreePath string `json:"worktree_path,omitempty"`
	// Payload contains type-specific event data
	Payload json.RawMessage `json:"payload,omitempty"`
}

// StreamEventType categorizes stream events.
type StreamEventType string

const (
	// Agent lifecycle events
	StreamEventAgentCreated    StreamEventType = "agent_created"
	StreamEventAgentStarted    StreamEventType = "agent_started"
	StreamEventAgentTerminated StreamEventType = "agent_terminated"
	StreamEventAgentCrashed    StreamEventType = "agent_crashed"
	StreamEventAgentAwaiting   StreamEventType = "agent_awaiting"

	// Agent activity events (from claude-code output)
	StreamEventThinking  StreamEventType = "thinking"      // Claude is thinking
	StreamEventToolCall  StreamEventType = "tool_call"     // Tool invocation started
	StreamEventToolResult StreamEventType = "tool_result"  // Tool completed
	StreamEventMessage   StreamEventType = "message"       // Assistant message
	StreamEventHeartbeat StreamEventType = "heartbeat"     // Keep-alive / activity ping

	// Job lifecycle events
	StreamEventJobCreated   StreamEventType = "job_created"
	StreamEventJobStarted   StreamEventType = "job_started"
	StreamEventJobCompleted StreamEventType = "job_completed"
	StreamEventJobFailed    StreamEventType = "job_failed"

	// Worktree events
	StreamEventWorktreeCreated  StreamEventType = "worktree_created"
	StreamEventWorktreePublished StreamEventType = "worktree_published"
	StreamEventWorktreeMerged   StreamEventType = "worktree_merged"
	StreamEventWorktreeCleaned  StreamEventType = "worktree_cleaned"

	// Plan events
	StreamEventPlanCreated  StreamEventType = "plan_created"
	StreamEventPlanApproved StreamEventType = "plan_approved"
	StreamEventPlanExecuting StreamEventType = "plan_executing"

	// System events
	StreamEventDaemonStarted StreamEventType = "daemon_started"
	StreamEventDaemonStopping StreamEventType = "daemon_stopping"
	StreamEventScanCompleted StreamEventType = "scan_completed"

	// API/Control events (for visualizing daemon RPC)
	StreamEventAPICall     StreamEventType = "api_call"     // RPC request received
	StreamEventAPIResponse StreamEventType = "api_response" // RPC response sent
)

// StreamSource identifies the component that emitted the event.
type StreamSource string

const (
	StreamSourceDaemon StreamSource = "daemon"   // athenad core
	StreamSourceAgent  StreamSource = "agent"    // Claude Code process
	StreamSourceStore  StreamSource = "store"    // SQLite operations
	StreamSourceAPI    StreamSource = "api"      // Control API
	StreamSourceClient StreamSource = "client"   // External client
)

// SubscribeStreamRequest is sent to initiate a stream subscription.
type SubscribeStreamRequest struct {
	// Filter by agent ID (optional)
	AgentID string `json:"agent_id,omitempty"`
	// Filter by worktree path (optional)
	WorktreePath string `json:"worktree_path,omitempty"`
	// Filter by event types (optional, empty = all)
	EventTypes []StreamEventType `json:"event_types,omitempty"`
	// Include historical events from the last N seconds (0 = none)
	HistorySeconds int `json:"history_seconds,omitempty"`
}

// --- Payload types for specific events ---

// ToolCallPayload contains details about a tool invocation.
type ToolCallPayload struct {
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
}

// ToolResultPayload contains the result of a tool invocation.
type ToolResultPayload struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	Duration  time.Duration   `json:"duration_ms,omitempty"`
}

// MessagePayload contains a message from the agent.
type MessagePayload struct {
	Role    string `json:"role"` // assistant, user, system
	Content string `json:"content"`
	Tokens  int    `json:"tokens,omitempty"`
}

// APICallPayload contains details about an API call.
type APICallPayload struct {
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
	ClientID string          `json:"client_id,omitempty"`
}

// APIResponsePayload contains details about an API response.
type APIResponsePayload struct {
	Method   string `json:"method"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"duration_ms"`
}

// HeartbeatPayload contains periodic status info.
type HeartbeatPayload struct {
	ActiveAgents int       `json:"active_agents"`
	PendingJobs  int       `json:"pending_jobs"`
	Uptime       int64     `json:"uptime_seconds"`
	LastScan     time.Time `json:"last_scan,omitempty"`
}

// NewStreamEvent creates a new StreamEvent with the given type and source.
func NewStreamEvent(eventType StreamEventType, source StreamSource) *StreamEvent {
	return &StreamEvent{
		ID:        generateStreamEventID(),
		Timestamp: time.Now(),
		Type:      eventType,
		Source:    source,
	}
}

// WithAgent sets the agent ID on the event.
func (e *StreamEvent) WithAgent(agentID string) *StreamEvent {
	e.AgentID = agentID
	return e
}

// WithWorktree sets the worktree path on the event.
func (e *StreamEvent) WithWorktree(path string) *StreamEvent {
	e.WorktreePath = path
	return e
}

// WithPayload sets the payload on the event.
func (e *StreamEvent) WithPayload(payload any) *StreamEvent {
	if payload != nil {
		data, _ := json.Marshal(payload)
		e.Payload = data
	}
	return e
}

// generateStreamEventID creates a unique event ID.
func generateStreamEventID() string {
	return time.Now().Format("20060102150405.000000")
}
