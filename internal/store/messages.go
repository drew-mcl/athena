package store

import (
	"database/sql"
	"time"

	"github.com/drewfead/athena/internal/data"
)

// CreateMessage inserts a new message into the database.
func (s *Store) CreateMessage(msg *data.Message) error {
	query := `
		INSERT INTO messages (
			id, agent_id, direction, type, sequence, timestamp,
			text, tool_name, tool_input, tool_output,
			error_code, error_message, session_id, raw
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var toolName, toolInput, toolOutput *string
	if msg.Tool != nil {
		toolName = &msg.Tool.Name
		if len(msg.Tool.Input) > 0 {
			s := string(msg.Tool.Input)
			toolInput = &s
		}
		if msg.Tool.Output != "" {
			toolOutput = &msg.Tool.Output
		}
	}

	var errorCode, errorMessage *string
	if msg.Error != nil {
		if msg.Error.Code != "" {
			errorCode = &msg.Error.Code
		}
		errorMessage = &msg.Error.Message
	}

	var rawStr *string
	if len(msg.Raw) > 0 {
		str := string(msg.Raw)
		rawStr = &str
	}

	_, err := s.db.Exec(query,
		msg.ID,
		msg.AgentID,
		msg.Direction,
		msg.Type,
		msg.Sequence,
		msg.Timestamp,
		nullString(msg.Text),
		toolName,
		toolInput,
		toolOutput,
		errorCode,
		errorMessage,
		nullString(msg.SessionID),
		rawStr,
	)
	return err
}

// GetMessage retrieves a message by ID.
func (s *Store) GetMessage(id string) (*data.Message, error) {
	query := `
		SELECT id, agent_id, direction, type, sequence, timestamp,
		       text, tool_name, tool_input, tool_output,
		       error_code, error_message, session_id, raw
		FROM messages WHERE id = ?
	`
	row := s.db.QueryRow(query, id)
	return scanMessage(row)
}

// GetMessages retrieves messages for an agent, ordered by sequence.
func (s *Store) GetMessages(agentID string, limit int) ([]*data.Message, error) {
	query := `
		SELECT id, agent_id, direction, type, sequence, timestamp,
		       text, tool_name, tool_input, tool_output,
		       error_code, error_message, session_id, raw
		FROM messages
		WHERE agent_id = ?
		ORDER BY sequence ASC
		LIMIT ?
	`
	rows, err := s.db.Query(query, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetMessagesByType retrieves messages of a specific type for an agent.
func (s *Store) GetMessagesByType(agentID string, msgType data.MessageType, limit int) ([]*data.Message, error) {
	query := `
		SELECT id, agent_id, direction, type, sequence, timestamp,
		       text, tool_name, tool_input, tool_output,
		       error_code, error_message, session_id, raw
		FROM messages
		WHERE agent_id = ? AND type = ?
		ORDER BY sequence ASC
		LIMIT ?
	`
	rows, err := s.db.Query(query, agentID, msgType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetRecentMessages retrieves the most recent N messages for an agent.
func (s *Store) GetRecentMessages(agentID string, n int) ([]*data.Message, error) {
	query := `
		SELECT id, agent_id, direction, type, sequence, timestamp,
		       text, tool_name, tool_input, tool_output,
		       error_code, error_message, session_id, raw
		FROM messages
		WHERE agent_id = ?
		ORDER BY sequence DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, agentID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}

	// Reverse to get chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// GetConversation retrieves all messages for an agent as a Conversation.
func (s *Store) GetConversation(agentID string) (*data.Conversation, error) {
	msgs, err := s.GetMessages(agentID, 100000) // reasonable limit
	if err != nil {
		return nil, err
	}

	conv := data.NewConversation(agentID)
	for _, msg := range msgs {
		conv.Messages = append(conv.Messages, msg)
	}

	if len(conv.Messages) > 0 {
		conv.StartedAt = conv.Messages[0].Timestamp
		if conv.IsComplete() {
			conv.EndedAt = &conv.Messages[len(conv.Messages)-1].Timestamp
		}
	}

	return conv, nil
}

// DeleteAgentMessages removes all messages for an agent.
func (s *Store) DeleteAgentMessages(agentID string) error {
	query := `DELETE FROM messages WHERE agent_id = ?`
	_, err := s.db.Exec(query, agentID)
	return err
}

// CountMessages returns the number of messages for an agent.
func (s *Store) CountMessages(agentID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM messages WHERE agent_id = ?`
	err := s.db.QueryRow(query, agentID).Scan(&count)
	return count, err
}

func scanMessage(row *sql.Row) (*data.Message, error) {
	var msg data.Message
	var text, toolName, toolInput, toolOutput sql.NullString
	var errorCode, errorMessage, sessionID, raw sql.NullString
	var timestamp time.Time

	err := row.Scan(
		&msg.ID,
		&msg.AgentID,
		&msg.Direction,
		&msg.Type,
		&msg.Sequence,
		&timestamp,
		&text,
		&toolName,
		&toolInput,
		&toolOutput,
		&errorCode,
		&errorMessage,
		&sessionID,
		&raw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	msg.Timestamp = timestamp
	msg.Text = text.String
	msg.SessionID = sessionID.String

	if toolName.Valid && toolName.String != "" {
		msg.Tool = &data.ToolContent{
			Name:   toolName.String,
			Output: toolOutput.String,
		}
		if toolInput.Valid {
			msg.Tool.Input = []byte(toolInput.String)
		}
	}

	if errorMessage.Valid && errorMessage.String != "" {
		msg.Error = &data.ErrorContent{
			Code:    errorCode.String,
			Message: errorMessage.String,
		}
	}

	if raw.Valid {
		msg.Raw = []byte(raw.String)
	}

	return &msg, nil
}

func scanMessages(rows *sql.Rows) ([]*data.Message, error) {
	var messages []*data.Message

	for rows.Next() {
		var msg data.Message
		var text, toolName, toolInput, toolOutput sql.NullString
		var errorCode, errorMessage, sessionID, raw sql.NullString
		var timestamp time.Time

		err := rows.Scan(
			&msg.ID,
			&msg.AgentID,
			&msg.Direction,
			&msg.Type,
			&msg.Sequence,
			&timestamp,
			&text,
			&toolName,
			&toolInput,
			&toolOutput,
			&errorCode,
			&errorMessage,
			&sessionID,
			&raw,
		)
		if err != nil {
			return nil, err
		}

		msg.Timestamp = timestamp
		msg.Text = text.String
		msg.SessionID = sessionID.String

		if toolName.Valid && toolName.String != "" {
			msg.Tool = &data.ToolContent{
				Name:   toolName.String,
				Output: toolOutput.String,
			}
			if toolInput.Valid {
				msg.Tool.Input = []byte(toolInput.String)
			}
		}

		if errorMessage.Valid && errorMessage.String != "" {
			msg.Error = &data.ErrorContent{
				Code:    errorCode.String,
				Message: errorMessage.String,
			}
		}

		if raw.Valid {
			msg.Raw = []byte(raw.String)
		}

		messages = append(messages, &msg)
	}

	return messages, rows.Err()
}

// nullString converts empty string to nil for SQL nullable fields.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
