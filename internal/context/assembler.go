package context

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drewfead/athena/internal/index"
)

// Assembler builds context blocks to prepend to agent prompts.
type Assembler struct {
	blackboard *Blackboard
	state      *StateStore
	index      *index.Index // Optional code index for relevant file lookup
}

// NewAssembler creates a new Assembler instance.
func NewAssembler(blackboard *Blackboard, state *StateStore) *Assembler {
	return &Assembler{
		blackboard: blackboard,
		state:      state,
	}
}

// WithIndex sets the code index for relevant file lookup.
func (a *Assembler) WithIndex(idx *index.Index) *Assembler {
	a.index = idx
	return a
}

// AssembleOptions configures context assembly.
type AssembleOptions struct {
	WorktreePath string
	ProjectName  string
	TicketID     string
	TaskPrompt   string // The task description (used for index queries)

	// Budget control
	MaxTokens int // Approximate token budget (0 = unlimited)

	// Content filters
	IncludeState         bool    // Include project-level state
	IncludeBlackboard    bool    // Include workflow-level blackboard
	IncludeRelevantFiles bool    // Include index-based relevant files
	MinStateConfidence   float64 // Minimum confidence for state entries (0-1)
	MaxRelevantFiles     int     // Maximum relevant files to include (default 10)
}

// DefaultAssembleOptions returns sensible defaults.
func DefaultAssembleOptions(worktreePath, projectName string) AssembleOptions {
	return AssembleOptions{
		WorktreePath:         worktreePath,
		ProjectName:          projectName,
		MaxTokens:            30000, // Default to ~30k tokens for robust context
		IncludeState:         true,
		IncludeBlackboard:    true,
		IncludeRelevantFiles: true, // Enable by default
		MinStateConfidence:   0.5,
		MaxRelevantFiles:     10,
	}
}

// WithTask returns options configured for a specific task.
func (opts AssembleOptions) WithTask(task string) AssembleOptions {
	opts.TaskPrompt = task
	return opts
}

// Assemble builds a context block from the blackboard and state stores.
func (a *Assembler) Assemble(opts AssembleOptions) (*ContextBlock, error) {
	block := &ContextBlock{
		ProjectName:  opts.ProjectName,
		WorktreePath: opts.WorktreePath,
		TicketID:     opts.TicketID,
		TaskPrompt:   opts.TaskPrompt,
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

	// Find relevant files using code index
	if opts.IncludeRelevantFiles && opts.TaskPrompt != "" && a.index != nil {
		maxFiles := opts.MaxRelevantFiles
		if maxFiles <= 0 {
			maxFiles = 10
		}
		scorer := index.NewScorer(a.index)
		results := scorer.ScoreFiles(opts.TaskPrompt, opts.WorktreePath, maxFiles)
		for _, r := range results {
			content := ""
			fullPath := filepath.Join(opts.WorktreePath, r.Path)
			if data, err := os.ReadFile(fullPath); err == nil {
				content = string(data)
				// Truncate if too large (e.g. > 4KB) to save tokens
				if len(content) > 4000 {
					content = content[:4000] + "\n... (truncated)"
				}
			}

			block.RelevantFiles = append(block.RelevantFiles, &RelevantFile{
				Path:    r.Path,
				Score:   r.Score,
				Reason:  r.Reason,
				Content: content,
			})
		}
	}

	// Generate project structure from the index (stable, cacheable content)
	if a.index != nil {
		block.ProjectStructure = a.generateProjectStructure()
	}

	// Calculate totals
	block.TotalEntries = len(block.StateEntries) +
		len(block.Decisions) +
		len(block.Findings) +
		len(block.Attempts) +
		len(block.Questions) +
		len(block.Artifacts) +
		len(block.RelevantFiles)

	return block, nil
}

// Format renders a context block as a markdown string.
//
// PROMPT CACHING STRATEGY:
// Claude caches prompts from the BEGINNING, so we order sections by stability:
//
// 1. STABLE (cached across agents working on same project):
//    - Project State: Architecture, conventions, constraints - rarely changes
//    - Project Structure: Directory layout and file counts - rarely changes
//
// 2. SEMI-STABLE (cached within a workflow):
//    - Relevant Files: Task-specific file suggestions
//    - Current Workflow: Decisions, findings, attempts - changes during workflow
//
// 3. UNIQUE (never cached - appended separately in BuildPromptWithContext):
//    - The actual task prompt
//
// This ordering maximizes cache hits when multiple agents work on the same
// project or when the same workflow spawns multiple agent invocations.
func (a *Assembler) Format(block *ContextBlock) string {
	if block.TotalEntries == 0 && block.ProjectStructure == "" {
		return "" // No context to add
	}

	var sb strings.Builder

	// ============================================================
	// STABLE SECTION - Cached across agents on the same project
	// ============================================================
	sb.WriteString("# Context (STABLE - cached across agents)\n\n")

	a.formatProjectState(&sb, block)
	a.formatProjectStructure(&sb, block)

	// Visual separator between stable and dynamic sections
	sb.WriteString("---\n\n")

	// ============================================================
	// SEMI-STABLE SECTION - Cached within a workflow
	// ============================================================
	sb.WriteString("# Workflow Context (SEMI-STABLE - cached within workflow)\n\n")

	a.formatRelevantFiles(&sb, block)
	a.formatWorkflowSections(&sb, block)

	return sb.String()
}

// formatRelevantFiles renders the relevant files section.
func (a *Assembler) formatRelevantFiles(sb *strings.Builder, block *ContextBlock) {
	if len(block.RelevantFiles) == 0 {
		return
	}
	sb.WriteString("## Relevant Files\n")
	sb.WriteString("These files are likely relevant to your task based on code analysis:\n\n")
	for _, f := range block.RelevantFiles {
		sb.WriteString(fmt.Sprintf("### `%s`\n", f.Path))
		sb.WriteString(fmt.Sprintf("_Reason: %s_\n\n", f.Reason))
		if f.Content != "" {
			sb.WriteString("```\n")
			sb.WriteString(f.Content)
			sb.WriteString("\n```\n\n")
		} else {
			sb.WriteString("(Content not available)\n\n")
		}
	}
	sb.WriteString("\n")
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

// formatProjectStructure renders the project structure section.
func (a *Assembler) formatProjectStructure(sb *strings.Builder, block *ContextBlock) {
	if block.ProjectStructure == "" {
		return
	}
	sb.WriteString("## Project Structure\n")
	sb.WriteString(block.ProjectStructure)
	sb.WriteString("\n")
}

// generateProjectStructure creates a summary of the codebase layout from the index.
// This information is stable and highly cacheable.
func (a *Assembler) generateProjectStructure() string {
	if a.index == nil {
		return ""
	}

	// Count files per top-level directory
	dirCounts := make(map[string]int)
	for filePath := range a.index.FileSymbols {
		// Get the top-level directory (e.g., "internal/daemon" -> "internal")
		parts := strings.Split(filePath, string(filepath.Separator))
		if len(parts) > 0 {
			topDir := parts[0]
			dirCounts[topDir]++
		}
	}

	if len(dirCounts) == 0 {
		return ""
	}

	// Sort directories for consistent output
	var dirs []string
	for dir := range dirCounts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var sb strings.Builder
	sb.WriteString("Key directories and file counts:\n")

	for _, dir := range dirs {
		// Get subdirectory breakdown for important directories
		if dir == "internal" || dir == "cmd" || dir == "pkg" {
			subDirs := a.getSubdirectories(dir)
			if len(subDirs) > 0 {
				sb.WriteString(fmt.Sprintf("- `%s/` (%d files total)\n", dir, dirCounts[dir]))
				for _, subDir := range subDirs {
					sb.WriteString(fmt.Sprintf("  - `%s`\n", subDir))
				}
			} else {
				sb.WriteString(fmt.Sprintf("- `%s/` (%d files)\n", dir, dirCounts[dir]))
			}
		} else {
			sb.WriteString(fmt.Sprintf("- `%s/` (%d files)\n", dir, dirCounts[dir]))
		}
	}

	return sb.String()
}

// getSubdirectories returns unique subdirectory names under a given top-level directory.
func (a *Assembler) getSubdirectories(topDir string) []string {
	subDirs := make(map[string]bool)

	for filePath := range a.index.FileSymbols {
		parts := strings.Split(filePath, string(filepath.Separator))
		if len(parts) >= 2 && parts[0] == topDir {
			subDirs[parts[1]] = true
		}
	}

	var result []string
	for subDir := range subDirs {
		result = append(result, subDir)
	}
	sort.Strings(result)
	return result
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
// The task is appended last as it is UNIQUE per invocation and never cached.
func (a *Assembler) BuildPromptWithContext(originalPrompt string, opts AssembleOptions) (string, error) {
	block, err := a.Assemble(opts)
	if err != nil {
		return "", err
	}

	// Apply token budget
	if opts.MaxTokens > 0 {
		block = a.TruncateToTokenBudget(block, opts.MaxTokens)
	}

	context := a.Format(block)
	if context == "" {
		return originalPrompt, nil
	}

	// Add separator between context and task (UNIQUE - never cached)
	return context + "---\n\n# Your Task (UNIQUE - never cached)\n" + originalPrompt, nil
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
	// Note: We preserve stable content (StateEntries, ProjectStructure) as long as possible
	// since they provide the best cache hit potential.
	trimmed := &ContextBlock{
		ProjectName:      block.ProjectName,
		WorktreePath:     block.WorktreePath,
		TicketID:         block.TicketID,
		TokenBudget:      maxTokens,
		StateEntries:     block.StateEntries,
		ProjectStructure: block.ProjectStructure,
		Decisions:        block.Decisions,
		Findings:         block.Findings,
		Attempts:         block.Attempts,
		Questions:        block.Questions,
		Artifacts:        block.Artifacts,
		RelevantFiles:    block.RelevantFiles,
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
