package data

import (
	"encoding/json"

	"github.com/drewfead/athena/internal/runner"
	"github.com/drewfead/athena/pkg/claudecode"
	"github.com/google/uuid"
)

// FromClaudeEvent converts a claudecode.Event to a Message.
// This normalizes Claude Code's event stream to the unified Message type.
func FromClaudeEvent(agentID string, seq int64, evt *claudecode.Event) *Message {
	raw, _ := json.Marshal(evt)

	return &Message{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Direction: Outbound,
		Type:      mapEventType(evt),
		Sequence:  seq,
		Timestamp: evt.Timestamp,
		Text:      evt.Content,
		Tool:      mapToolContent(evt),
		Error:     mapErrorContent(evt),
		SessionID: evt.SessionID,
		Raw:       raw,
	}
}

// mapEventType converts claudecode event type/subtype to MessageType.
func mapEventType(evt *claudecode.Event) MessageType {
	switch evt.Type {
	case claudecode.EventTypeSystem:
		return TypeSystem

	case claudecode.EventTypeAssistant:
		if evt.Subtype == "thinking" {
			return TypeThinking
		}
		return TypeText

	case claudecode.EventTypeToolUse:
		return TypeToolCall

	case claudecode.EventTypeToolResult:
		return TypeToolResult

	case claudecode.EventTypeResult:
		if evt.IsError() {
			return TypeError
		}
		return TypeComplete

	case claudecode.EventTypeError:
		return TypeError

	default:
		return TypeText
	}
}

// mapToolContent extracts tool information from the event.
func mapToolContent(evt *claudecode.Event) *ToolContent {
	if evt.Type != claudecode.EventTypeToolUse && evt.Type != claudecode.EventTypeToolResult {
		return nil
	}

	tc := &ToolContent{
		Name:  evt.Name,
		Input: evt.Input,
	}

	// Tool result has output in Content
	if evt.Type == claudecode.EventTypeToolResult {
		tc.Output = evt.Content
	}

	return tc
}

// mapErrorContent extracts error information from the event.
func mapErrorContent(evt *claudecode.Event) *ErrorContent {
	if !evt.IsError() {
		return nil
	}

	return &ErrorContent{
		Message: evt.Content,
	}
}

// ToConversation builds a Conversation from a sequence of Claude events.
func ToConversation(agentID string, events []*claudecode.Event) *Conversation {
	conv := NewConversation(agentID)

	for i, evt := range events {
		msg := FromClaudeEvent(agentID, int64(i), evt)
		conv.Messages = append(conv.Messages, msg)
	}

	if len(conv.Messages) > 0 {
		conv.StartedAt = conv.Messages[0].Timestamp
		if conv.IsComplete() {
			conv.EndedAt = &conv.Messages[len(conv.Messages)-1].Timestamp
		}
	}

	return conv
}

// EventProcessor processes Claude events and converts them to messages.
type EventProcessor struct {
	AgentID string
	seq     int64
}

// NewEventProcessor creates a processor for a specific agent.
func NewEventProcessor(agentID string) *EventProcessor {
	return &EventProcessor{AgentID: agentID}
}

// Process converts an event to a message and increments sequence.
func (ep *EventProcessor) Process(evt *claudecode.Event) *Message {
	msg := FromClaudeEvent(ep.AgentID, ep.seq, evt)
	ep.seq++
	return msg
}

// Sequence returns the current sequence number.
func (ep *EventProcessor) Sequence() int64 {
	return ep.seq
}

// FromRunnerEvent converts a runner.Event to a Message.
func FromRunnerEvent(agentID string, seq int64, evt runner.Event) *Message {
	raw, _ := json.Marshal(evt)

	return &Message{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Direction: Outbound,
		Type:      mapRunnerEventType(evt),
		Sequence:  seq,
		Timestamp: evt.Timestamp,
		Text:      evt.Content,
		Tool:      mapRunnerToolContent(evt),
		Error:     mapRunnerErrorContent(evt),
		SessionID: evt.SessionID,
		Raw:       raw,
		Usage:     mapRunnerUsage(evt.Usage),
	}
}

func mapRunnerUsage(u *runner.EventUsage) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		CacheReads:   u.CacheReads,
	}
}

func mapRunnerEventType(evt runner.Event) MessageType {
	switch evt.Type {
	case "system":
		return TypeSystem

	case "assistant":
		if evt.Subtype == "thinking" {
			return TypeThinking
		}
		return TypeText

	case "tool_use":
		return TypeToolCall

	case "tool_result":
		return TypeToolResult

	case "result":
		if evt.Subtype == "error" {
			return TypeError
		}
		return TypeComplete

	case "error":
		return TypeError

	default:
		return TypeText
	}
}

func mapRunnerToolContent(evt runner.Event) *ToolContent {
	if evt.Type != "tool_use" && evt.Type != "tool_result" {
		return nil
	}

	tc := &ToolContent{
		Name:  evt.Name,
		Input: evt.Input,
	}

	if evt.Type == "tool_result" {
		tc.Output = evt.Content
	}

	return tc
}

func mapRunnerErrorContent(evt runner.Event) *ErrorContent {
	if evt.Type != "error" && (evt.Type != "result" || evt.Subtype != "error") {
		return nil
	}

	return &ErrorContent{
		Message: evt.Content,
	}
}
