// Package claudecode provides a wrapper around the Claude Code CLI.
package claudecode

import (
	"encoding/json"
	"time"
)

// EventType represents the type of event from Claude Code.
type EventType string

const (
	EventTypeSystem     EventType = "system"
	EventTypeAssistant  EventType = "assistant"
	EventTypeUser       EventType = "user"
	EventTypeToolUse    EventType = "tool_use"
	EventTypeToolResult EventType = "tool_result"
	EventTypeResult     EventType = "result"
	EventTypeError      EventType = "error"
)

// Event represents a parsed event from Claude Code's stream-json output.
type Event struct {
	Type      EventType       `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Timestamp time.Time       `json:"-"`
}

// SystemEvent is sent at the start of a session.
type SystemEvent struct {
	SessionID string `json:"session_id"`
	Subtype   string `json:"subtype"` // "init"
}

// AssistantEvent represents assistant output (thinking, text).
type AssistantEvent struct {
	Subtype string `json:"subtype"` // "thinking", "text"
	Content string `json:"content"`
}

// ToolUseEvent represents a tool invocation.
type ToolUseEvent struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResultEvent represents a tool's response.
type ToolResultEvent struct {
	Content string `json:"content"`
}

// ResultEvent represents the final result.
type ResultEvent struct {
	Subtype string `json:"subtype"` // "success", "error"
	Content string `json:"content"`
}

// ParseEvent parses a raw JSON line into an Event.
func ParseEvent(data []byte) (*Event, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	event.Timestamp = time.Now()
	return &event, nil
}

// IsComplete returns true if this event indicates the session ended.
func (e *Event) IsComplete() bool {
	return e.Type == EventTypeResult
}

// IsSuccess returns true if this is a successful completion.
func (e *Event) IsSuccess() bool {
	return e.Type == EventTypeResult && e.Subtype == "success"
}

// IsError returns true if this event represents an error.
func (e *Event) IsError() bool {
	return e.Type == EventTypeError || (e.Type == EventTypeResult && e.Subtype == "error")
}

// IsThinking returns true if the assistant is thinking.
func (e *Event) IsThinking() bool {
	return e.Type == EventTypeAssistant && e.Subtype == "thinking"
}

// IsToolUse returns true if this is a tool invocation.
func (e *Event) IsToolUse() bool {
	return e.Type == EventTypeToolUse
}
