# Context Optimization Design

**Goal:** Reduce agent token usage by giving them focused, relevant context - no ML models required.

## The Problem

Agents burn tokens by:
1. Reading files they don't need
2. Re-reading the same files across turns
3. Exploring broadly when task is narrow
4. No visibility into what's costing us

## Solutions (No Models Required)

### 1. Dependency Graph Index

Build a static map of "file A depends on file B" using AST parsing.

```
internal/daemon/daemon.go
  ├── imports: control, agent, store, logging, config
  ├── defines: Daemon, NewDaemon, Start, Stop
  └── calls: store.GetAgent, agent.Spawn, logging.Info

internal/control/client.go
  ├── imports: (stdlib only)
  ├── defines: Client, AgentStatus, SpawnRequest
  └── used_by: daemon/daemon.go, tui/dashboard/model.go
```

**How it helps:** Task mentions "fix agent spawning" → graph says `agent/spawn.go` + `daemon/daemon.go` + `control/client.go` → only those files go in context.

**Implementation:**
- Go: `go/ast` + `go/parser` (stdlib, zero deps)
- Other langs: tree-sitter (C library with Go bindings)
- Store as JSON/SQLite, rebuild on file change

---

### 2. Symbol Index

Fast lookup of "where is X defined?" and "where is X used?"

```json
{
  "symbols": {
    "Daemon": {
      "kind": "struct",
      "file": "internal/daemon/daemon.go",
      "line": 42,
      "references": [
        "cmd/athenad/main.go:15",
        "internal/daemon/handlers.go:8"
      ]
    },
    "SpawnAgent": {
      "kind": "func",
      "file": "internal/agent/spawn.go",
      "line": 23,
      "references": [...]
    }
  }
}
```

**How it helps:** Agent asks "where is SpawnAgent defined?" → instant lookup instead of grep + read + grep + read.

**Implementation:**
- `gopls` can dump this (but heavy)
- Or build lightweight version with `go/ast`
- ctags/universal-ctags as fallback for any language

---

### 3. File Relevance Scoring

Given a task description, score files by likely relevance:

```go
type RelevanceSignals struct {
    PathMatch      float64  // "auth" in path, task mentions "auth"
    SymbolMatch    float64  // task mentions "SpawnAgent", file defines it
    DependencyHops int      // how many imports away from known-relevant files
    RecentlyChanged bool    // git shows recent activity
    FileSize       int      // prefer smaller, focused files
}

func ScoreFile(task string, file string, index *Index) float64 {
    // Combine signals, return 0-1 score
}
```

**How it helps:** Before agent starts, compute top 10-20 relevant files. Pre-load those into context. Agent starts focused.

---

### 4. Usage Tracking & Metrics

Capture what Claude Code / agents actually report.

```go
type AgentMetrics struct {
    SessionID       string

    // Token usage (from API response)
    InputTokens     int
    OutputTokens    int
    CacheReadTokens int
    CacheWriteTokens int

    // Tool usage
    ToolCalls       int
    ToolSuccesses   int
    ToolFailures    int

    // File access patterns
    FilesRead       []string
    FilesModified   []string

    // Timing
    WallTime        time.Duration
    APITime         time.Duration
    ToolTime        time.Duration

    // Cost (computed)
    EstimatedCost   float64
}
```

**How to capture:**
- Parse agent stdout/stderr for summary blocks
- Or: if spawning via API, capture response metadata directly
- Store in SQLite, aggregate over time

---

### 5. TUI Metrics Dashboard

New tab or panel showing agent efficiency:

```
┌─────────────────────────────────────────────────────────────────────┐
│ Agent Metrics                                            [d]etails │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│ Session: drew/eng-456-fix-auth    Status: Completed                │
│ Duration: 12m 34s (API: 2m 10s, Tools: 45s, Idle: 9m 39s)         │
│                                                                     │
│ Tokens                                                              │
│ ├── Input:      48,291  ████████████████████░░░░  (68%)            │
│ ├── Cached:     22,104  ████████████░░░░░░░░░░░░  (45% hit rate)   │
│ └── Output:      3,847  ███░░░░░░░░░░░░░░░░░░░░░                   │
│                                                                     │
│ Files: 12 read, 3 modified    Tools: 24 calls (✓23 ✗1)             │
│ Est. Cost: $0.42                                                    │
│                                                                     │
│ Top Files by Token Cost                                             │
│ 1. internal/daemon/daemon.go      8,421 tokens (read 4x)           │
│ 2. internal/agent/spawn.go        6,102 tokens (read 3x)           │
│ 3. internal/control/client.go     4,887 tokens (read 2x)           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Success Metrics

| Metric | Target | How to Measure |
|--------|--------|----------------|
| Cache hit rate | >50% | `cache_read / input_tokens` |
| Tokens per task | Decreasing trend | Rolling average |
| Files read/modified ratio | <5:1 | Agent isn't exploring aimlessly |
| Re-read rate | <2x per file | Same file shouldn't be read repeatedly |
| Tool success rate | >90% | Agent isn't flailing |
| Cost per task | Track baseline, improve | Sum API costs |

---

## Implementation Phases

### Phase 1: Metrics & Visibility
- [ ] Parse agent output for token/tool stats
- [ ] Store metrics in SQLite
- [ ] Add metrics panel to TUI
- [ ] Establish baselines

### Phase 2: Symbol & Dependency Index
- [ ] Build Go AST parser for symbol extraction
- [ ] Generate dependency graph
- [ ] Store index in `~/.athena/indices/<project>/`
- [ ] CLI: `athena index build`, `athena index query "SpawnAgent"`

### Phase 3: Smart Context Injection
- [ ] Score files by relevance to task
- [ ] Pre-compute relevant file list before agent spawn
- [ ] Inject via blackboard or system prompt
- [ ] Measure token reduction

### Phase 4: Cross-Agent Optimization
- [ ] Shared context prefix across agents in same project
- [ ] Leverage Claude's prompt caching with stable prefixes
- [ ] Track cross-agent cache hit rates

---

## Data Model

```sql
-- Agent session metrics
CREATE TABLE agent_metrics (
    id TEXT PRIMARY KEY,
    worktree_id TEXT,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,

    input_tokens INTEGER,
    output_tokens INTEGER,
    cache_read_tokens INTEGER,
    cache_write_tokens INTEGER,

    tool_calls INTEGER,
    tool_successes INTEGER,

    files_read TEXT,      -- JSON array
    files_modified TEXT,  -- JSON array

    wall_time_ms INTEGER,
    api_time_ms INTEGER,
    tool_time_ms INTEGER,

    estimated_cost_cents INTEGER
);

-- File access patterns (for optimization)
CREATE TABLE file_access (
    session_id TEXT,
    file_path TEXT,
    access_count INTEGER,
    tokens_consumed INTEGER,
    PRIMARY KEY (session_id, file_path)
);

-- Symbol index (rebuilt on change)
CREATE TABLE symbols (
    project_hash TEXT,
    symbol_name TEXT,
    kind TEXT,           -- func, struct, interface, const, var
    file_path TEXT,
    line_number INTEGER,
    PRIMARY KEY (project_hash, symbol_name, file_path)
);

-- Dependency graph
CREATE TABLE dependencies (
    project_hash TEXT,
    from_file TEXT,
    to_file TEXT,
    dep_type TEXT,       -- import, call, embed
    PRIMARY KEY (project_hash, from_file, to_file)
);
```

---

## Open Questions

1. **How to parse agent output?**
   - Claude Code outputs a summary at end - can we capture it?
   - Or do we need to intercept API calls directly?

2. **Index rebuild triggers?**
   - On daemon startup?
   - On file save (fsnotify)?
   - On git commit?

3. **Multi-language support?**
   - Go: stdlib `go/ast` ✓
   - Other langs: tree-sitter or ctags?
   - Start Go-only, expand later?
