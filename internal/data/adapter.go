package data

import (
	"encoding/json"

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
