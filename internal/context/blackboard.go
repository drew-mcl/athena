package context

import (
	"time"

	"github.com/drewfead/athena/internal/store"
	"github.com/google/uuid"
)

// Blackboard provides operations on the ephemeral, worktree-scoped entry store.
type Blackboard struct {
	store *store.Store
}

// NewBlackboard creates a new Blackboard instance.
func NewBlackboard(st *store.Store) *Blackboard {
	return &Blackboard{store: st}
}

// PostDecision records a decision made during the workflow.
func (b *Blackboard) PostDecision(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	entry := &BlackboardEntry{
		ID:           uuid.NewString(),
		WorktreePath: worktreePath,
		EntryType:    BlackboardTypeDecision,
		Content:      content,
		AgentID:      agentID,
	}
	if err := b.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// PostFinding records a discovery made during the workflow.
func (b *Blackboard) PostFinding(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	entry := &BlackboardEntry{
		ID:           uuid.NewString(),
		WorktreePath: worktreePath,
		EntryType:    BlackboardTypeFinding,
		Content:      content,
		AgentID:      agentID,
	}
	if err := b.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// PostAttempt records what was tried during the workflow.
func (b *Blackboard) PostAttempt(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	entry := &BlackboardEntry{
		ID:           uuid.NewString(),
		WorktreePath: worktreePath,
		EntryType:    BlackboardTypeAttempt,
		Content:      content,
		AgentID:      agentID,
	}
	if err := b.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// PostQuestion records an open question during the workflow.
func (b *Blackboard) PostQuestion(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	entry := &BlackboardEntry{
		ID:           uuid.NewString(),
		WorktreePath: worktreePath,
		EntryType:    BlackboardTypeQuestion,
		Content:      content,
		AgentID:      agentID,
		Resolved:     false,
	}
	if err := b.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// PostArtifact records a created artifact during the workflow.
func (b *Blackboard) PostArtifact(worktreePath, agentID, content string) (*BlackboardEntry, error) {
	entry := &BlackboardEntry{
		ID:           uuid.NewString(),
		WorktreePath: worktreePath,
		EntryType:    BlackboardTypeArtifact,
		Content:      content,
		AgentID:      agentID,
	}
	if err := b.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// Post creates a blackboard entry of the specified type.
func (b *Blackboard) Post(worktreePath, agentID string, entryType BlackboardEntryType, content string) (*BlackboardEntry, error) {
	entry := &BlackboardEntry{
		ID:           uuid.NewString(),
		WorktreePath: worktreePath,
		EntryType:    entryType,
		Content:      content,
		AgentID:      agentID,
	}
	if err := b.store.CreateBlackboardEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// Get retrieves a blackboard entry by ID.
func (b *Blackboard) Get(id string) (*BlackboardEntry, error) {
	return b.store.GetBlackboardEntry(id)
}

// List retrieves all entries for a worktree.
func (b *Blackboard) List(worktreePath string) ([]*BlackboardEntry, error) {
	return b.store.ListBlackboardEntries(worktreePath)
}

// ListByType retrieves entries filtered by type.
func (b *Blackboard) ListByType(worktreePath string, entryType BlackboardEntryType) ([]*BlackboardEntry, error) {
	return b.store.ListBlackboardEntriesByType(worktreePath, entryType)
}

// ListDecisions retrieves all decision entries.
func (b *Blackboard) ListDecisions(worktreePath string) ([]*BlackboardEntry, error) {
	return b.store.ListBlackboardEntriesByType(worktreePath, BlackboardTypeDecision)
}

// ListFindings retrieves all finding entries.
func (b *Blackboard) ListFindings(worktreePath string) ([]*BlackboardEntry, error) {
	return b.store.ListBlackboardEntriesByType(worktreePath, BlackboardTypeFinding)
}

// ListAttempts retrieves all attempt entries.
func (b *Blackboard) ListAttempts(worktreePath string) ([]*BlackboardEntry, error) {
	return b.store.ListBlackboardEntriesByType(worktreePath, BlackboardTypeAttempt)
}

// ListUnresolvedQuestions retrieves unresolved question entries.
func (b *Blackboard) ListUnresolvedQuestions(worktreePath string) ([]*BlackboardEntry, error) {
	return b.store.ListUnresolvedQuestions(worktreePath)
}

// ListArtifacts retrieves all artifact entries.
func (b *Blackboard) ListArtifacts(worktreePath string) ([]*BlackboardEntry, error) {
	return b.store.ListBlackboardEntriesByType(worktreePath, BlackboardTypeArtifact)
}

// ResolveQuestion marks a question as resolved.
func (b *Blackboard) ResolveQuestion(id string, resolvedBy string) error {
	return b.store.ResolveBlackboardQuestion(id, resolvedBy)
}

// Clear removes all entries for a worktree.
func (b *Blackboard) Clear(worktreePath string) error {
	return b.store.ClearBlackboard(worktreePath)
}

// Delete removes a single entry.
func (b *Blackboard) Delete(id string) error {
	return b.store.DeleteBlackboardEntry(id)
}

// Summary returns statistics for a worktree's blackboard.
func (b *Blackboard) Summary(worktreePath string) (*BlackboardSummary, error) {
	counts, err := b.store.CountBlackboardEntries(worktreePath)
	if err != nil {
		return nil, err
	}

	// Count unresolved questions
	unresolved, err := b.store.ListUnresolvedQuestions(worktreePath)
	if err != nil {
		return nil, err
	}

	summary := &BlackboardSummary{
		WorktreePath:    worktreePath,
		DecisionCount:   counts[BlackboardTypeDecision],
		FindingCount:    counts[BlackboardTypeFinding],
		AttemptCount:    counts[BlackboardTypeAttempt],
		QuestionCount:   counts[BlackboardTypeQuestion],
		UnresolvedCount: len(unresolved),
		ArtifactCount:   counts[BlackboardTypeArtifact],
		LastUpdated:     time.Now(),
	}
	summary.TotalCount = summary.DecisionCount + summary.FindingCount + summary.AttemptCount + summary.QuestionCount + summary.ArtifactCount

	return summary, nil
}

// GetGrouped returns entries grouped by type for easy access.
func (b *Blackboard) GetGrouped(worktreePath string) (decisions, findings, attempts, questions, artifacts []*BlackboardEntry, err error) {
	entries, err := b.List(worktreePath)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	for _, e := range entries {
		switch e.EntryType {
		case BlackboardTypeDecision:
			decisions = append(decisions, e)
		case BlackboardTypeFinding:
			findings = append(findings, e)
		case BlackboardTypeAttempt:
			attempts = append(attempts, e)
		case BlackboardTypeQuestion:
			if !e.Resolved {
				questions = append(questions, e)
			}
		case BlackboardTypeArtifact:
			artifacts = append(artifacts, e)
		}
	}
	return
}
