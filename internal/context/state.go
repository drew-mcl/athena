package context

import (
	"github.com/drewfead/athena/internal/store"
	"github.com/google/uuid"
)

// StateStore provides operations on the durable, project-scoped fact store.
type StateStore struct {
	store *store.Store
}

// NewStateStore creates a new StateStore instance.
func NewStateStore(st *store.Store) *StateStore {
	return &StateStore{store: st}
}

// SetArchitecture records an architectural pattern for the project.
func (s *StateStore) SetArchitecture(project, key, value string, agentID *string, ref *string) (*StateEntry, error) {
	entry := &StateEntry{
		ID:          uuid.NewString(),
		Project:     project,
		StateType:   StateTypeArchitecture,
		Key:         key,
		Value:       value,
		Confidence:  1.0,
		SourceAgent: agentID,
		SourceRef:   ref,
	}
	if err := s.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// SetConvention records a code convention for the project.
func (s *StateStore) SetConvention(project, key, value string, agentID *string, ref *string) (*StateEntry, error) {
	entry := &StateEntry{
		ID:          uuid.NewString(),
		Project:     project,
		StateType:   StateTypeConvention,
		Key:         key,
		Value:       value,
		Confidence:  1.0,
		SourceAgent: agentID,
		SourceRef:   ref,
	}
	if err := s.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// SetConstraint records a hard constraint for the project.
func (s *StateStore) SetConstraint(project, key, value string, agentID *string, ref *string) (*StateEntry, error) {
	entry := &StateEntry{
		ID:          uuid.NewString(),
		Project:     project,
		StateType:   StateTypeConstraint,
		Key:         key,
		Value:       value,
		Confidence:  1.0,
		SourceAgent: agentID,
		SourceRef:   ref,
	}
	if err := s.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// SetDecision records an architectural decision for the project.
func (s *StateStore) SetDecision(project, key, value string, agentID *string, ref *string) (*StateEntry, error) {
	entry := &StateEntry{
		ID:          uuid.NewString(),
		Project:     project,
		StateType:   StateTypeDecision,
		Key:         key,
		Value:       value,
		Confidence:  1.0,
		SourceAgent: agentID,
		SourceRef:   ref,
	}
	if err := s.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// SetEnvironment records an environment fact for the project.
func (s *StateStore) SetEnvironment(project, key, value string, agentID *string, ref *string) (*StateEntry, error) {
	entry := &StateEntry{
		ID:          uuid.NewString(),
		Project:     project,
		StateType:   StateTypeEnvironment,
		Key:         key,
		Value:       value,
		Confidence:  1.0,
		SourceAgent: agentID,
		SourceRef:   ref,
	}
	if err := s.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// Set creates or updates a state entry of the specified type.
func (s *StateStore) Set(project string, stateType StateEntryType, key, value string, confidence float64, agentID *string, ref *string) (*StateEntry, error) {
	entry := &StateEntry{
		ID:          uuid.NewString(),
		Project:     project,
		StateType:   stateType,
		Key:         key,
		Value:       value,
		Confidence:  confidence,
		SourceAgent: agentID,
		SourceRef:   ref,
	}
	if err := s.store.UpsertStateEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// Get retrieves a state entry by ID.
func (s *StateStore) Get(id string) (*StateEntry, error) {
	return s.store.GetStateEntry(id)
}

// GetByKey retrieves a state entry by project, type, and key.
func (s *StateStore) GetByKey(project string, stateType StateEntryType, key string) (*StateEntry, error) {
	return s.store.GetStateEntryByKey(project, stateType, key)
}

// List retrieves all active state entries for a project.
func (s *StateStore) List(project string) ([]*StateEntry, error) {
	return s.store.ListStateEntries(project)
}

// ListByType retrieves state entries filtered by type.
func (s *StateStore) ListByType(project string, stateType StateEntryType) ([]*StateEntry, error) {
	return s.store.ListStateEntriesByType(project, stateType)
}

// ListArchitecture retrieves all architecture entries.
func (s *StateStore) ListArchitecture(project string) ([]*StateEntry, error) {
	return s.store.ListStateEntriesByType(project, StateTypeArchitecture)
}

// ListConventions retrieves all convention entries.
func (s *StateStore) ListConventions(project string) ([]*StateEntry, error) {
	return s.store.ListStateEntriesByType(project, StateTypeConvention)
}

// ListConstraints retrieves all constraint entries.
func (s *StateStore) ListConstraints(project string) ([]*StateEntry, error) {
	return s.store.ListStateEntriesByType(project, StateTypeConstraint)
}

// ListDecisions retrieves all decision entries.
func (s *StateStore) ListDecisions(project string) ([]*StateEntry, error) {
	return s.store.ListStateEntriesByType(project, StateTypeDecision)
}

// ListEnvironment retrieves all environment entries.
func (s *StateStore) ListEnvironment(project string) ([]*StateEntry, error) {
	return s.store.ListStateEntriesByType(project, StateTypeEnvironment)
}

// UpdateValue updates the value and confidence of a state entry.
func (s *StateStore) UpdateValue(id string, value string, confidence float64) error {
	return s.store.UpdateStateValue(id, value, confidence)
}

// Delete removes a state entry.
func (s *StateStore) Delete(id string) error {
	return s.store.DeleteStateEntry(id)
}

// DeleteAll removes all state entries for a project.
func (s *StateStore) DeleteAll(project string) error {
	return s.store.DeleteProjectState(project)
}

// GetHighConfidence retrieves entries above a confidence threshold.
func (s *StateStore) GetHighConfidence(project string, minConfidence float64) ([]*StateEntry, error) {
	return s.store.GetHighConfidenceState(project, minConfidence)
}

// Summary returns statistics for a project's state entries.
func (s *StateStore) Summary(project string) (*StateSummary, error) {
	counts, err := s.store.CountStateEntries(project)
	if err != nil {
		return nil, err
	}

	entries, err := s.store.ListStateEntries(project)
	if err != nil {
		return nil, err
	}

	var totalConfidence float64
	for _, e := range entries {
		totalConfidence += e.Confidence
	}

	avgConfidence := 0.0
	if len(entries) > 0 {
		avgConfidence = totalConfidence / float64(len(entries))
	}

	summary := &StateSummary{
		Project:           project,
		ArchitectureCount: counts[StateTypeArchitecture],
		ConventionCount:   counts[StateTypeConvention],
		ConstraintCount:   counts[StateTypeConstraint],
		DecisionCount:     counts[StateTypeDecision],
		EnvironmentCount:  counts[StateTypeEnvironment],
		TotalCount:        len(entries),
		AvgConfidence:     avgConfidence,
	}

	return summary, nil
}

// GetGrouped returns entries grouped by type for easy access.
func (s *StateStore) GetGrouped(project string) (architecture, conventions, constraints, decisions, environment []*StateEntry, err error) {
	entries, err := s.List(project)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	for _, e := range entries {
		switch e.StateType {
		case StateTypeArchitecture:
			architecture = append(architecture, e)
		case StateTypeConvention:
			conventions = append(conventions, e)
		case StateTypeConstraint:
			constraints = append(constraints, e)
		case StateTypeDecision:
			decisions = append(decisions, e)
		case StateTypeEnvironment:
			environment = append(environment, e)
		}
	}
	return
}
