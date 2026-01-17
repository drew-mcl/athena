package data

import (
	"slices"
	"time"
)

// Conversation is an ordered sequence of messages for an agent session.
type Conversation struct {
	AgentID   string
	Messages  []*Message
	StartedAt time.Time
	EndedAt   *time.Time
}

// NewConversation creates a new empty conversation.
func NewConversation(agentID string) *Conversation {
	return &Conversation{
		AgentID:   agentID,
		Messages:  make([]*Message, 0),
		StartedAt: time.Now(),
	}
}

// Append adds a message with auto-incrementing sequence.
func (c *Conversation) Append(msg *Message) {
	msg.Sequence = int64(len(c.Messages))
	c.Messages = append(c.Messages, msg)

	// Update end time on terminal messages
	if msg.IsTerminal() {
		now := time.Now()
		c.EndedAt = &now
	}
}

// Len returns the number of messages.
func (c *Conversation) Len() int {
	return len(c.Messages)
}

// IsComplete returns true if conversation has a terminal message.
func (c *Conversation) IsComplete() bool {
	if len(c.Messages) == 0 {
		return false
	}
	return c.Messages[len(c.Messages)-1].IsTerminal()
}

// IsSuccess returns true if conversation completed successfully.
func (c *Conversation) IsSuccess() bool {
	if len(c.Messages) == 0 {
		return false
	}
	last := c.Messages[len(c.Messages)-1]
	return last.Type == TypeComplete
}

// Last returns the most recent message, or nil if empty.
func (c *Conversation) Last() *Message {
	if len(c.Messages) == 0 {
		return nil
	}
	return c.Messages[len(c.Messages)-1]
}

// LastN returns up to n most recent messages.
func (c *Conversation) LastN(n int) []*Message {
	if len(c.Messages) <= n {
		return c.Messages
	}
	return c.Messages[len(c.Messages)-n:]
}

// Filter returns messages matching the given types.
func (c *Conversation) Filter(types ...MessageType) []*Message {
	result := make([]*Message, 0)
	for _, msg := range c.Messages {
		if slices.Contains(types, msg.Type) {
			result = append(result, msg)
		}
	}
	return result
}

// ToolCalls returns all tool call messages.
func (c *Conversation) ToolCalls() []*Message {
	return c.Filter(TypeToolCall)
}

// Errors returns all error messages.
func (c *Conversation) Errors() []*Message {
	return c.Filter(TypeError)
}

// Duration returns the conversation duration, or time since start if not ended.
func (c *Conversation) Duration() time.Duration {
	if c.EndedAt != nil {
		return c.EndedAt.Sub(c.StartedAt)
	}
	return time.Since(c.StartedAt)
}

// GetMessage retrieves a message by ID.
func (c *Conversation) GetMessage(id string) *Message {
	for _, msg := range c.Messages {
		if msg.ID == id {
			return msg
		}
	}
	return nil
}

// Summary returns a brief summary of the conversation.
type ConversationSummary struct {
	AgentID       string
	MessageCount  int
	ToolCallCount int
	ErrorCount    int
	IsComplete    bool
	Duration      time.Duration
}

// Summary generates a summary of the conversation.
func (c *Conversation) Summary() ConversationSummary {
	return ConversationSummary{
		AgentID:       c.AgentID,
		MessageCount:  len(c.Messages),
		ToolCallCount: len(c.ToolCalls()),
		ErrorCount:    len(c.Errors()),
		IsComplete:    c.IsComplete(),
		Duration:      c.Duration(),
	}
}
