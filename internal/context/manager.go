package context

import (
	"fmt"
	"strings"

	"github.com/drewfead/athena/internal/store"
	"github.com/google/uuid"
)

// Manager is the main interface for agent context management.
// It coordinates between the blackboard, state store, parser, and assembler.
type Manager struct {
	blackboard *Blackboard
	state      *StateStore
	parser     *Parser
	assembler  *Assembler
	store      *store.Store
}

// NewManager creates a new context Manager.
func NewManager(st *store.Store) *Manager {
	blackboard := NewBlackboard(st)
	state := NewStateStore(st)
	parser := NewParser()
	assembler := NewAssembler(blackboard, state)

	return &Manager{
		blackboard: blackboard,
		state:      state,
		parser:     parser,
		assembler:  assembler,
		store:      st,
	}
}

// Blackboard returns the blackboard for direct access.
func (m *Manager) Blackboard() *Blackboard {
	return m.blackboard
}

// State returns the state store for direct access.
func (m *Manager) State() *StateStore {
	return m.state
}

// Parser returns the parser for direct access.
func (m *Manager) Parser() *Parser {
	return m.parser
}

// Assembler returns the assembler for direct access.
func (m *Manager) Assembler() *Assembler {
	return m.assembler
}

// AssembleContext builds a context block for a worktree.
func (m *Manager) AssembleContext(worktreePath, projectName string) (*ContextBlock, error) {
	opts := DefaultAssembleOptions(worktreePath, projectName)
	return m.assembler.Assemble(opts)
}

// AssembleContextWithOpts builds a context block with custom options.
func (m *Manager) AssembleContextWithOpts(opts AssembleOptions) (*ContextBlock, error) {
	return m.assembler.Assemble(opts)
}

// FormatContext renders a context block as markdown.
func (m *Manager) FormatContext(block *ContextBlock) string {
	return m.assembler.Format(block)
}

// BuildPromptWithContext prepends context to a prompt.
func (m *Manager) BuildPromptWithContext(originalPrompt, worktreePath, projectName string) (string, error) {
	opts := DefaultAssembleOptions(worktreePath, projectName)
	return m.assembler.BuildPromptWithContext(originalPrompt, opts)
}

// ParseAgentOutput extracts markers from agent output and records them.
func (m *Manager) ParseAgentOutput(worktreePath, projectName, agentID, output string) ([]*ParsedMarker, error) {
	markers := m.parser.Parse(output)

	for _, marker := range markers {
		if err := m.recordMarker(worktreePath, projectName, agentID, marker); err != nil {
			// Log but continue processing other markers
			continue
		}
	}

	return markers, nil
}

// recordMarker saves a parsed marker to the appropriate store.
func (m *Manager) recordMarker(worktreePath, projectName, agentID string, marker *ParsedMarker) error {
	switch marker.MarkerType {
	case MarkerTypeDecision, MarkerTypeFinding, MarkerTypeTried, MarkerTypeQuestion:
		// Map to blackboard entry type
		entryType, ok := MapMarkerTypeToBlackboard(marker.MarkerType)
		if !ok {
			return fmt.Errorf("unknown marker type: %s", marker.MarkerType)
		}

		content := m.parser.FormatForBlackboard(marker)
		_, err := m.blackboard.Post(worktreePath, agentID, entryType, content)
		return err

	case MarkerTypeState:
		// Map to state entry
		stateTypeStr, ok := marker.Metadata["state_type"]
		if !ok {
			return fmt.Errorf("state marker missing type")
		}
		stateType, ok := MapMarkerTypeToState(stateTypeStr)
		if !ok {
			return fmt.Errorf("unknown state type: %s", stateTypeStr)
		}

		key, ok := marker.Metadata["key"]
		if !ok {
			return fmt.Errorf("state marker missing key")
		}

		_, err := m.state.Set(projectName, stateType, key, marker.Content, 1.0, &agentID, nil)
		return err

	default:
		return fmt.Errorf("unhandled marker type: %s", marker.MarkerType)
	}
}

// PostFromMarkers records multiple markers at once.
func (m *Manager) PostFromMarkers(worktreePath, projectName, agentID string, markers []*ParsedMarker) error {
	for _, marker := range markers {
		if err := m.recordMarker(worktreePath, projectName, agentID, marker); err != nil {
			// Continue on error, but we could accumulate errors
			continue
		}
	}
	return nil
}

// ClearWorkflow clears all blackboard entries for a worktree.
// Call this when a workflow completes or is abandoned.
func (m *Manager) ClearWorkflow(worktreePath string) error {
	return m.blackboard.Clear(worktreePath)
}

// GetBlackboardSummary returns statistics for a worktree's blackboard.
func (m *Manager) GetBlackboardSummary(worktreePath string) (*BlackboardSummary, error) {
	return m.blackboard.Summary(worktreePath)
}

// GetStateSummary returns statistics for a project's state.
func (m *Manager) GetStateSummary(projectName string) (*StateSummary, error) {
	return m.state.Summary(projectName)
}

// GetContextPreview returns a formatted preview of what context an agent would see.
func (m *Manager) GetContextPreview(worktreePath, projectName string) (string, error) {
	block, err := m.AssembleContext(worktreePath, projectName)
	if err != nil {
		return "", err
	}
	return m.FormatContext(block), nil
}

// RecordDecision posts a decision to the blackboard.
func (m *Manager) RecordDecision(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	return m.blackboard.PostDecision(worktreePath, agentID, content)
}

// RecordFinding posts a finding to the blackboard.
func (m *Manager) RecordFinding(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	return m.blackboard.PostFinding(worktreePath, agentID, content)
}

// RecordAttempt posts an attempt to the blackboard.
func (m *Manager) RecordAttempt(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	return m.blackboard.PostAttempt(worktreePath, agentID, content)
}

// RecordQuestion posts a question to the blackboard.
func (m *Manager) RecordQuestion(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	return m.blackboard.PostQuestion(worktreePath, agentID, content)
}

// RecordArtifact posts an artifact to the blackboard.
func (m *Manager) RecordArtifact(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	return m.blackboard.PostArtifact(worktreePath, agentID, content)
}

// ResolveQuestion marks a question as resolved.
func (m *Manager) ResolveQuestion(questionID, resolvedByAgentID string) error {
	return m.blackboard.ResolveQuestion(questionID, resolvedByAgentID)
}

// SetProjectState creates or updates a project state entry.
func (m *Manager) SetProjectState(projectName string, stateType StateEntryType, key, value string, agentID *string) (*StateEntry, error) {
	return m.state.Set(projectName, stateType, key, value, 1.0, agentID, nil)
}

// GetProjectState retrieves all state entries for a project.
func (m *Manager) GetProjectState(projectName string) ([]*StateEntry, error) {
	return m.state.List(projectName)
}

// BatchPost creates multiple blackboard entries at once.
func (m *Manager) BatchPost(worktreePath, agentID string, entries []struct {
	EntryType BlackboardEntryType
	Content   string
}) ([]*BlackboardEntry, error) {
	var results []*BlackboardEntry
	for _, e := range entries {
		entry, err := m.blackboard.Post(worktreePath, agentID, e.EntryType, e.Content)
		if err != nil {
			return results, err
		}
		results = append(results, entry)
	}
	return results, nil
}

// ExportBlackboard returns all blackboard entries for a worktree as a formatted string.
func (m *Manager) ExportBlackboard(worktreePath string) (string, error) {
	entries, err := m.blackboard.List(worktreePath)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Blackboard Export: %s\n\n", worktreePath))

	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("## %s (%s)\n", e.EntryType, e.AgentID))
		sb.WriteString(e.Content)
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}

// ExportState returns all state entries for a project as a formatted string.
func (m *Manager) ExportState(projectName string) (string, error) {
	entries, err := m.state.List(projectName)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# State Export: %s\n\n", projectName))

	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("## %s/%s\n", e.StateType, e.Key))
		sb.WriteString(fmt.Sprintf("Value: %s\n", e.Value))
		sb.WriteString(fmt.Sprintf("Confidence: %.2f\n", e.Confidence))
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// GenerateEntryID generates a unique ID for entries.
func GenerateEntryID() string {
	return uuid.NewString()
}
