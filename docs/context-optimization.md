# Context System Future Enhancements

This document captures future optimization ideas for Athena's agent context system (blackboard + state store).

## Current Implementation

The context system provides shared memory for agents:

- **Blackboard**: Ephemeral, worktree-scoped entries (decisions, findings, attempts, questions, artifacts)
- **State Store**: Durable, project-scoped facts (architecture, conventions, constraints, decisions, environment)

## Future Enhancements

### 1. Artifact Indexing

Index code symbols from agent-created artifacts for retrieval:

```
Agent creates: internal/auth/jwt.go
System extracts: functions (CreateToken, ValidateToken), types (TokenClaims)
Later agents can query: "Where is JWT validation implemented?"
```

**Implementation notes:**
- Parse Go files with `go/parser` to extract symbols
- Store in a `symbols` table with file path, symbol name, type, line number
- Query symbols when assembling context for related work

### 2. Semantic Search

Add embeddings for "find related" queries:

```
Query: "authentication patterns"
Returns: Related decisions, findings from similar work
```

**Implementation notes:**
- Embed blackboard entries and state entries
- Store embeddings in SQLite (or separate vector store)
- Use cosine similarity for retrieval
- Budget: ~100 most relevant entries per context assembly

### 3. Cross-Project Learning

Share conventions and patterns across projects:

```
Project A learns: "Use structured logging with slog"
Project B (same owner) sees: "Convention from similar projects: structured logging with slog"
```

**Implementation notes:**
- Add `global_state` table for cross-project knowledge
- Confidence weighting based on source project similarity
- Opt-in per project (some codebases are unique)

### 4. Confidence Decay

Reduce confidence of old state entries over time:

```
Initial: confidence=1.0 (just discovered)
After 30 days: confidence=0.8
After 90 days: confidence=0.6
```

**Implementation notes:**
- Add `last_validated` timestamp to state entries
- Decay function: `confidence * (0.99 ^ days_since_validation)`
- Agents can "validate" entries when they confirm them true
- Low-confidence entries get filtered from context assembly

### 5. Conflict Resolution

Handle contradictory state entries:

```
Entry 1: "Architecture: monolith"
Entry 2: "Architecture: microservices"
```

**Implementation notes:**
- Detect conflicts by same state_type + key but different value
- Surface conflicts in TUI for human resolution
- Track resolution history (who decided, when, why)
- Option: Agent proposes resolution based on codebase analysis

### 6. Smarter Context Budgeting

Current: Fixed token budget (~2K tokens)
Future: Dynamic budgeting based on task complexity:

```
Simple bugfix: 500 tokens (recent attempts, relevant decisions)
New feature: 2000 tokens (full context)
Architecture change: 4000 tokens (all project state, recent history)
```

**Implementation notes:**
- Estimate task complexity from prompt analysis
- Allocate budget proportionally
- Prioritization: state > decisions > findings > attempts > questions

### 7. Agent Feedback Loop

Let agents mark context entries as helpful or not:

```
Agent receives context with "JWT implementation at internal/auth/"
Agent uses that information → marks as helpful
Agent finds it outdated → marks as stale
```

**Implementation notes:**
- Add `helpful_count`, `stale_count` to entries
- Weight entries by helpfulness score in assembly
- Auto-archive entries with high stale count

### 8. Context Visualization

Enhanced TUI views:

- Timeline view: Show context evolution over workflow
- Graph view: Show relationships between entries
- Diff view: Show what changed since last agent run

### 9. Export/Import

Support exporting and importing context:

```bash
# Export project context for backup/sharing
athena context export --project myapp > context.json

# Import context into new project
athena context import context.json --project myapp-v2
```

### 10. Metrics and Analytics

Track context system effectiveness:

- Average context size per spawn
- Most referenced entries
- Entry survival rate (how long before superseded)
- Correlation between context size and task success

## Implementation Priority

| Enhancement | Impact | Effort | Priority |
|-------------|--------|--------|----------|
| Artifact Indexing | High | Medium | P1 |
| Confidence Decay | Medium | Low | P1 |
| Context Visualization | Medium | Medium | P2 |
| Smarter Budgeting | Medium | Medium | P2 |
| Semantic Search | High | High | P3 |
| Conflict Resolution | Medium | Medium | P3 |
| Agent Feedback | Medium | Low | P3 |
| Cross-Project | High | High | P4 |
| Export/Import | Low | Low | P4 |
| Metrics | Low | Medium | P4 |

## Design Principles

1. **Additive, not disruptive**: Enhancements should extend the system without breaking existing workflows
2. **Graceful degradation**: If embeddings fail, fall back to keyword matching
3. **Human override**: Users can always manually edit/clear context
4. **Transparency**: Show what context agents receive and how it was selected
