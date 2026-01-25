// Package context manages shared agent memory for Athena.
// It provides a blackboard for ephemeral, worktree-scoped entries
// and a state store for durable, project-scoped facts.
package context

import (
	"time"

	"github.com/drewfead/athena/internal/store"
)

// Re-export types from store for convenience
type (
	BlackboardEntry     = store.BlackboardEntry
	BlackboardEntryType = store.BlackboardEntryType
	StateEntry          = store.StateEntry
	StateEntryType      = store.StateEntryType
)

// Re-export constants
const (
	BlackboardTypeDecision = store.BlackboardTypeDecision
	BlackboardTypeFinding  = store.BlackboardTypeFinding
	BlackboardTypeAttempt  = store.BlackboardTypeAttempt
	BlackboardTypeQuestion = store.BlackboardTypeQuestion
	BlackboardTypeArtifact = store.BlackboardTypeArtifact

	StateTypeArchitecture = store.StateTypeArchitecture
	StateTypeConvention   = store.StateTypeConvention
	StateTypeConstraint   = store.StateTypeConstraint
	StateTypeDecision     = store.StateTypeDecision
	StateTypeEnvironment  = store.StateTypeEnvironment
)

// ParsedMarker represents a structured marker extracted from agent output.
type ParsedMarker struct {
	MarkerType MarkerType
	Title      string            // Short title/description
	Content    string            // Full content
	Metadata   map[string]string // Optional metadata (e.g., reference, outcome)
}

// MarkerType identifies the kind of marker extracted from agent output.
type MarkerType string

const (
	MarkerTypeDecision MarkerType = "DECISION"
	MarkerTypeFinding  MarkerType = "FINDING"
	MarkerTypeTried    MarkerType = "TRIED"
	MarkerTypeQuestion MarkerType = "QUESTION"
	MarkerTypeState    MarkerType = "STATE" // For updating project state
)

// RelevantFile represents a file identified as relevant to the current task.
type RelevantFile struct {
	Path    string  // Relative path from project root
	Score   float64 // Relevance score (0-1)
	Reason  string  // Why this file is relevant
	Content string  // Content of the file (truncated if large)
}

// ContextBlock represents the assembled context to prepend to agent prompts.
type ContextBlock struct {
	ProjectName  string
	WorktreePath string
	TicketID     string
	TaskPrompt   string // The task being performed (for index queries)

	// Project-level state (STABLE - cached across agents)
	StateEntries     []*StateEntry
	ProjectStructure string // Brief description of key directories and file counts

	// Workflow-level blackboard (SEMI-STABLE - cached within workflow)
	Decisions []*BlackboardEntry
	Findings  []*BlackboardEntry
	Attempts  []*BlackboardEntry
	Questions []*BlackboardEntry // Unresolved questions only
	Artifacts []*BlackboardEntry

	// Index-based relevant files (SEMI-STABLE - task-specific)
	RelevantFiles []*RelevantFile

	// Statistics
	TotalEntries int
	TokenBudget  int // Approximate token count for context
}

// AttemptOutcome represents the result of an attempt.
type AttemptOutcome string

const (
	AttemptOutcomeFailed  AttemptOutcome = "failed"
	AttemptOutcomePartial AttemptOutcome = "partial"
	AttemptOutcomeBlocked AttemptOutcome = "blocked"
)

// BlackboardSummary provides statistics about blackboard entries.
type BlackboardSummary struct {
	WorktreePath     string
	DecisionCount    int
	FindingCount     int
	AttemptCount     int
	QuestionCount    int // Total questions
	UnresolvedCount  int // Unresolved questions
	ArtifactCount    int
	TotalCount       int
	LastUpdated      time.Time
}

// StateSummary provides statistics about project state entries.
type StateSummary struct {
	Project           string
	ArchitectureCount int
	ConventionCount   int
	ConstraintCount   int
	DecisionCount     int
	EnvironmentCount  int
	TotalCount        int
	AvgConfidence     float64
}

// ContextStats provides statistics about the context that would be provided to an agent.
// This is used for cache hit rate tracking and analysis.
type ContextStats struct {
	// State section (STABLE - cached across agents on same project)
	StateEntriesCount   int
	StateTokensEstimate int

	// Blackboard section (SEMI-STABLE - cached within workflow)
	BlackboardEntriesCount   int
	BlackboardTokensEstimate int

	// Total context
	TotalContextTokens int
}
