// Package data provides unified bidirectional data models for agent I/O.
//
// The data plane normalizes all communication to/from agents into a consistent
// Message type. Inbound messages (prompts) and outbound messages (responses)
// use the same structure, enabling conversation replay and debugging.
package data

import (
	"encoding/json"
	"time"
)

// Direction indicates message flow.
type Direction string

const (
	// Inbound messages flow TO the agent (prompts, follow-ups).
	Inbound Direction = "in"

	// Outbound messages flow FROM the agent (responses, events).
	Outbound Direction = "out"
)

// MessageType categorizes the message content.
type MessageType string

const (
	// TypePrompt is an initial or follow-up instruction to the agent.
	TypePrompt MessageType = "prompt"

	// TypeSystem is a system-level message (session init, config).
	TypeSystem MessageType = "system"

	// TypeThinking is agent reasoning/thinking output.
	TypeThinking MessageType = "thinking"

	// TypeText is text output from the agent.
	TypeText MessageType = "text"

	// TypeToolCall is a tool invocation by the agent.
	TypeToolCall MessageType = "tool_call"

	// TypeToolResult is the response from a tool.
	TypeToolResult MessageType = "tool_result"

	// TypeComplete indicates successful session completion.
	TypeComplete MessageType = "complete"

	// TypeError indicates an error (terminal).
	TypeError MessageType = "error"
)

// Message is the unified data unit for agent I/O.
type Message struct {
	ID        string      `json:"id"`
	AgentID   string      `json:"agent_id"`
	Direction Direction   `json:"direction"`
	Type      MessageType `json:"type"`
	Sequence  int64       `json:"sequence"`  // Ordering within conversation
	Timestamp time.Time   `json:"timestamp"`

	// Content (one of these based on Type)
	Text  string        `json:"text,omitempty"`
	Tool  *ToolContent  `json:"tool,omitempty"`
	Error *ErrorContent `json:"error,omitempty"`

	// Metadata
	SessionID string          `json:"session_id,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"` // Original event for debugging
	Usage     *Usage          `json:"usage,omitempty"`
}

// Usage tracks token consumption for a message.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CacheReads   int `json:"cache_reads,omitempty"`
}

// ToolContent holds tool call/result data.
type ToolContent struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output string          `json:"output,omitempty"`
}

// ErrorContent holds error information.
type ErrorContent struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// IsTerminal returns true if this message type ends the conversation.
func (m *Message) IsTerminal() bool {
	return m.Type == TypeComplete || m.Type == TypeError
}

// IsFromAgent returns true if this message originated from the agent.
func (m *Message) IsFromAgent() bool {
	return m.Direction == Outbound
}

// IsToAgent returns true if this message was sent to the agent.
func (m *Message) IsToAgent() bool {
	return m.Direction == Inbound
}

// GetContent returns the primary text content of the message.
func (m *Message) GetContent() string {
	if m.Text != "" {
		return m.Text
	}
	if m.Tool != nil && m.Tool.Output != "" {
		return m.Tool.Output
	}
	if m.Error != nil {
		return m.Error.Message
	}
	return ""
}

// NewPromptMessage creates an inbound prompt message.
func NewPromptMessage(id, agentID, sessionID, text string, seq int64) *Message {
	return &Message{
		ID:        id,
		AgentID:   agentID,
		Direction: Inbound,
		Type:      TypePrompt,
		Sequence:  seq,
		Timestamp: time.Now(),
		Text:      text,
		SessionID: sessionID,
	}
}

// NewTextMessage creates an outbound text message.
func NewTextMessage(id, agentID, sessionID, text string, seq int64) *Message {
	return &Message{
		ID:        id,
		AgentID:   agentID,
		Direction: Outbound,
		Type:      TypeText,
		Sequence:  seq,
		Timestamp: time.Now(),
		Text:      text,
		SessionID: sessionID,
	}
}

// NewErrorMessage creates an outbound error message.
func NewErrorMessage(id, agentID, sessionID, errMsg string, seq int64) *Message {
	return &Message{
		ID:        id,
		AgentID:   agentID,
		Direction: Outbound,
		Type:      TypeError,
		Sequence:  seq,
		Timestamp: time.Now(),
		Error:     &ErrorContent{Message: errMsg},
		SessionID: sessionID,
	}
}
