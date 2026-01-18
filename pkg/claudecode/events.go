// Package claudecode provides a wrapper around the Claude Code CLI.
package claudecode

import (
	"encoding/json"
	"strings"
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

// messageWrapper represents Claude CLI's nested message format.
type messageWrapper struct {
	Content []contentBlock `json:"content"`
}

// contentBlock represents items in message.content array.
type contentBlock struct {
	Type    string          `json:"type"`            // "text", "tool_use", or "tool_result"
	Text    string          `json:"text,omitempty"`  // for text blocks
	Name    string          `json:"name,omitempty"`  // for tool_use blocks
	Input   json.RawMessage `json:"input,omitempty"` // for tool_use blocks
	Content json.RawMessage `json:"content,omitempty"` // for tool_result (can be string or array)
}

// ParseEvent parses a raw JSON line into an Event.
// Handles Claude CLI's nested message format:
//
//	{"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}
//	{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{}}]}}
//	{"type":"user","message":{"content":[{"type":"tool_result","content":"..."}]}}
func ParseEvent(data []byte) (*Event, error) {
	// Parse the raw envelope first
	var raw struct {
		Type      EventType       `json:"type"`
		Subtype   string          `json:"subtype,omitempty"`
		SessionID string          `json:"session_id,omitempty"`
		Content   string          `json:"content,omitempty"` // for flat format fallback
		Name      string          `json:"name,omitempty"`    // for flat format fallback
		Input     json.RawMessage `json:"input,omitempty"`   // for flat format fallback
		Message   *messageWrapper `json:"message,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	event := &Event{
		Type:      raw.Type,
		Subtype:   raw.Subtype,
		SessionID: raw.SessionID,
		Content:   raw.Content,
		Name:      raw.Name,
		Input:     raw.Input,
		Timestamp: time.Now(),
	}

	// Handle nested message.content for assistant/user types
	if raw.Message != nil && len(raw.Message.Content) > 0 {
		for _, cb := range raw.Message.Content {
			switch cb.Type {
			case "text":
				event.Subtype = "text"
				if event.Content == "" {
					event.Content = cb.Text
				} else {
					event.Content += cb.Text
				}
			case "tool_use":
				event.Type = EventTypeToolUse
				event.Name = cb.Name
				event.Input = cb.Input
			case "tool_result":
				event.Type = EventTypeToolResult
				event.Content = extractToolResultContent(cb.Content)
			}
		}
	}

	return event, nil
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

// extractToolResultContent handles tool_result content which can be:
// - A plain string: "content": "output text"
// - An array of content blocks: "content": [{"type":"text","text":"output"}]
func extractToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first (most common case)
	var strContent string
	if err := json.Unmarshal(raw, &strContent); err == nil {
		return strContent
	}

	// Try array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var result strings.Builder
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(b.Text)
			}
		}
		return result.String()
	}

	// Fallback: return raw JSON as string
	return string(raw)
}
