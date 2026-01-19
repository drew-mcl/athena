package context

import (
	"fmt"
	"strings"
)

// Assembler builds context blocks to prepend to agent prompts.
type Assembler struct {
	blackboard *Blackboard
	state      *StateStore
}

// NewAssembler creates a new Assembler instance.
func NewAssembler(blackboard *Blackboard, state *StateStore) *Assembler {
	return &Assembler{
		blackboard: blackboard,
		state:      state,
	}
}

// AssembleOptions configures context assembly.
type AssembleOptions struct {
	WorktreePath string
	ProjectName  string
	TicketID     string

	// Budget control
	MaxTokens int // Approximate token budget (0 = unlimited)

	// Content filters
	IncludeState       bool // Include project-level state
	IncludeBlackboard  bool // Include workflow-level blackboard
	MinStateConfidence float64 // Minimum confidence for state entries (0-1)
}

// DefaultAssembleOptions returns sensible defaults.
func DefaultAssembleOptions(worktreePath, projectName string) AssembleOptions {
	return AssembleOptions{
		WorktreePath:       worktreePath,
		ProjectName:        projectName,
		MaxTokens:          2000, // ~1-2K tokens for context
		IncludeState:       true,
		IncludeBlackboard:  true,
		MinStateConfidence: 0.5,
	}
}

// Assemble builds a context block from the blackboard and state stores.
func (a *Assembler) Assemble(opts AssembleOptions) (*ContextBlock, error) {
	block := &ContextBlock{
		ProjectName:  opts.ProjectName,
		WorktreePath: opts.WorktreePath,
		TicketID:     opts.TicketID,
		TokenBudget:  opts.MaxTokens,
	}

	// Load project state
	if opts.IncludeState && opts.ProjectName != "" {
		entries, err := a.state.GetHighConfidence(opts.ProjectName, opts.MinStateConfidence)
		if err != nil {
			return nil, fmt.Errorf("failed to load state: %w", err)
		}
		block.StateEntries = entries
	}

	// Load blackboard entries
	if opts.IncludeBlackboard && opts.WorktreePath != "" {
		decisions, findings, attempts, questions, artifacts, err := a.blackboard.GetGrouped(opts.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load blackboard: %w", err)
		}
		block.Decisions = decisions
		block.Findings = findings
		block.Attempts = attempts
		block.Questions = questions
		block.Artifacts = artifacts
	}

	// Calculate totals
	block.TotalEntries = len(block.StateEntries) +
		len(block.Decisions) +
		len(block.Findings) +
		len(block.Attempts) +
		len(block.Questions) +
		len(block.Artifacts)

	return block, nil
}

// Format renders a context block as a markdown string.
func (a *Assembler) Format(block *ContextBlock) string {
	if block.TotalEntries == 0 {
		return "" // No context to add
	}

	var sb strings.Builder
	sb.WriteString("# Context\n\n")

	a.formatProjectState(&sb, block)
	a.formatWorkflowSections(&sb, block)

	return sb.String()
}

// formatProjectState renders the project state section.
func (a *Assembler) formatProjectState(sb *strings.Builder, block *ContextBlock) {
	if len(block.StateEntries) == 0 {
		return
	}
	sb.WriteString("## Project State\n")
	a.formatStateEntries(sb, block.StateEntries)
	sb.WriteString("\n")
}

// formatWorkflowSections renders all blackboard sections.
func (a *Assembler) formatWorkflowSections(sb *strings.Builder, block *ContextBlock) {
	sections := []struct {
		title   string
		entries []*BlackboardEntry
	}{
		{"Decisions Made", block.Decisions},
		{"Findings", block.Findings},
		{"What Was Tried", block.Attempts},
		{"Open Questions", block.Questions},
		{"Created Artifacts", block.Artifacts},
	}

	// Check if any sections have entries
	hasEntries := false
	for _, s := range sections {
		if len(s.entries) > 0 {
			hasEntries = true
			break
		}
	}
	if !hasEntries {
		return
	}

	sb.WriteString("## Current Workflow\n")
	a.formatWorkflowHeader(sb, block)

	for _, s := range sections {
		a.formatBlackboardSection(sb, s.title, s.entries)
	}
}

// formatWorkflowHeader renders the worktree and ticket info.
func (a *Assembler) formatWorkflowHeader(sb *strings.Builder, block *ContextBlock) {
	if block.WorktreePath != "" {
		sb.WriteString(fmt.Sprintf("Worktree: %s\n", block.WorktreePath))
	}
	if block.TicketID != "" {
		sb.WriteString(fmt.Sprintf("Ticket: %s\n", block.TicketID))
	}
	sb.WriteString("\n")
}

// formatBlackboardSection renders a single blackboard section if it has entries.
func (a *Assembler) formatBlackboardSection(sb *strings.Builder, title string, entries []*BlackboardEntry) {
	if len(entries) == 0 {
		return
	}
	sb.WriteString("### " + title + "\n")
	a.formatBlackboardEntries(sb, entries)
	sb.WriteString("\n")
}

// formatStateEntries renders state entries grouped by type.
func (a *Assembler) formatStateEntries(sb *strings.Builder, entries []*StateEntry) {
	// Group by type
	byType := make(map[StateEntryType][]*StateEntry)
	for _, e := range entries {
		byType[e.StateType] = append(byType[e.StateType], e)
	}

	// Order of types to display
	typeOrder := []StateEntryType{
		StateTypeArchitecture,
		StateTypeConvention,
		StateTypeConstraint,
		StateTypeDecision,
		StateTypeEnvironment,
	}

	typeLabels := map[StateEntryType]string{
		StateTypeArchitecture: "Architecture",
		StateTypeConvention:   "Conventions",
		StateTypeConstraint:   "Constraints",
		StateTypeDecision:     "Decisions",
		StateTypeEnvironment:  "Environment",
	}

	for _, stateType := range typeOrder {
		typeEntries, ok := byType[stateType]
		if !ok || len(typeEntries) == 0 {
			continue
		}

		for _, e := range typeEntries {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", typeLabels[stateType], e.Value))
		}
	}
}

// formatBlackboardEntries renders blackboard entries as a numbered list.
func (a *Assembler) formatBlackboardEntries(sb *strings.Builder, entries []*BlackboardEntry) {
	for i, e := range entries {
		// Get archetype from agent ID if we had agent lookup
		// For now, just show the content
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, a.formatEntry(e)))
	}
}

// formatEntry formats a single blackboard entry.
func (a *Assembler) formatEntry(e *BlackboardEntry) string {
	content := e.Content

	// Truncate long content for context block
	if len(content) > 200 {
		content = content[:197] + "..."
	}

	return content
}

// BuildPromptWithContext prepends context to an existing prompt.
func (a *Assembler) BuildPromptWithContext(originalPrompt string, opts AssembleOptions) (string, error) {
	block, err := a.Assemble(opts)
	if err != nil {
		return "", err
	}

	context := a.Format(block)
	if context == "" {
		return originalPrompt, nil
	}

	// Add separator between context and task
	return context + "---\n\n# Your Task\n" + originalPrompt, nil
}

// EstimateTokens provides a rough token count for the context block.
// Uses ~4 chars per token as a rough estimate.
func EstimateTokens(text string) int {
	return len(text) / 4
}

// TruncateToTokenBudget truncates the context to fit within a token budget.
func (a *Assembler) TruncateToTokenBudget(block *ContextBlock, maxTokens int) *ContextBlock {
	if maxTokens <= 0 {
		return block // No truncation
	}

	// First render to check size
	text := a.Format(block)
	currentTokens := EstimateTokens(text)

	if currentTokens <= maxTokens {
		return block // Already within budget
	}

	// Create a copy and start trimming from least important
	trimmed := &ContextBlock{
		ProjectName:  block.ProjectName,
		WorktreePath: block.WorktreePath,
		TicketID:     block.TicketID,
		TokenBudget:  maxTokens,
		StateEntries: block.StateEntries,
		Decisions:    block.Decisions,
		Findings:     block.Findings,
		Attempts:     block.Attempts,
		Questions:    block.Questions,
		Artifacts:    block.Artifacts,
	}

	// Priority for trimming (least important first):
	// 1. Artifacts (can be rediscovered)
	// 2. Old findings (keep recent ones)
	// 3. Old attempts (keep recent ones)
	// 4. Low-confidence state entries

	// Trim artifacts first
	trimmed.Artifacts = nil
	text = a.Format(trimmed)
	if EstimateTokens(text) <= maxTokens {
		return trimmed
	}

	// Trim findings to most recent 5
	if len(trimmed.Findings) > 5 {
		trimmed.Findings = trimmed.Findings[len(trimmed.Findings)-5:]
	}
	text = a.Format(trimmed)
	if EstimateTokens(text) <= maxTokens {
		return trimmed
	}

	// Trim attempts to most recent 3
	if len(trimmed.Attempts) > 3 {
		trimmed.Attempts = trimmed.Attempts[len(trimmed.Attempts)-3:]
	}
	text = a.Format(trimmed)
	if EstimateTokens(text) <= maxTokens {
		return trimmed
	}

	// Trim decisions to most recent 5
	if len(trimmed.Decisions) > 5 {
		trimmed.Decisions = trimmed.Decisions[len(trimmed.Decisions)-5:]
	}
	text = a.Format(trimmed)
	if EstimateTokens(text) <= maxTokens {
		return trimmed
	}

	// Last resort: trim state entries to top 5 by confidence
	if len(trimmed.StateEntries) > 5 {
		trimmed.StateEntries = trimmed.StateEntries[:5]
	}

	return trimmed
}
