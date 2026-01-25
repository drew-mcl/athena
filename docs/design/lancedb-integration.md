# LanceDB Integration Design

## Problem Statement

Athena agents need semantic search over project codebases to provide relevant context. Currently, agents rely on:
- Grep/ripgrep for exact matches
- File path patterns
- Manual context injection

This misses semantic relationships and requires agents to "know" what to look for.

## Key Design Questions

### 1. Index Scope: Per-Project vs Per-Worktree

**Option A: Per-Project Index (Recommended)**
```
~/.athena/indices/
  └── <project-hash>/
      └── vectors.lance/
```
- Single index for the main branch
- All worktrees share the same base index
- Worktree-specific changes are ephemeral

**Option B: Per-Worktree Index**
```
<worktree>/.athena/
  └── vectors.lance/
```
- Each worktree has its own index
- Massive duplication
- Sync complexity

**Decision needed:** Per-project seems right, but how do we handle worktree-specific files?

---

### 2. When to Update the Index

| Trigger | Pros | Cons |
|---------|------|------|
| On worktree creation | Fresh context | Slow startup |
| On PR merge to main | Source of truth stays current | Stale during long features |
| On file save (watched) | Always fresh | CPU/battery cost |
| On daemon startup | Predictable | Could be stale |
| Manual/scheduled | User control | Easy to forget |

**Proposed hybrid:**
1. Full reindex on daemon startup (if stale > 24h)
2. Incremental update on PR merge (webhook or git hook)
3. Optional manual refresh via TUI

---

### 3. Consistent State & Source of Truth

```
Main Branch Index
       │
       ├──► Worktree A (reads from main index)
       │         └── Local overlay (uncommitted changes)
       │
       └──► Worktree B (reads from main index)
                 └── Local overlay (uncommitted changes)
```

**Strategy:**
- Main index is the source of truth
- Worktrees can have a "dirty overlay" for new/modified files
- Overlay is discarded on worktree cleanup
- On PR merge, main index absorbs the changes

---

### 4. PR Merge Integration

**Option A: Post-merge hook**
```bash
# .git/hooks/post-merge
athena index update --incremental
```

**Option B: CI/CD integration**
- GitHub Action triggers daemon via webhook
- Daemon pulls and reindexes

**Option C: Polling**
- Daemon checks `git log` periodically
- Reindexes if HEAD moved

---

### 5. Local Development Flow

```
Developer workflow:
1. Creates worktree → Gets main index + empty overlay
2. Writes new files → Files added to overlay (on save or on-demand)
3. Queries semantic search → Searches main + overlay
4. PR merged → Overlay discarded, main reindexed
5. Worktree cleaned → Overlay deleted
```

**Open question:** Should overlay indexing be automatic or opt-in?

---

### 6. TUI Integration

**New Tab: "Index" or extend "Settings"?**

```
┌─────────────────────────────────────────────────┐
│ Index Status                                    │
├─────────────────────────────────────────────────┤
│ Project: athena                                 │
│ Last indexed: 2 hours ago                       │
│ Files: 127 │ Vectors: 3,421 │ Size: 12MB       │
│                                                 │
│ Overlay (worktree: lancedb-exploration)         │
│ Modified: 3 files │ New: 1 file                │
│                                                 │
│ [r] Refresh index  [o] Rebuild overlay         │
│ [f] Force full reindex                          │
└─────────────────────────────────────────────────┘
```

**Agent view enhancement:**
- Show "context sources" when agent is working
- Visualize which files were retrieved via semantic search

---

### 7. Success Metrics

| Metric | How to Measure |
|--------|----------------|
| **Retrieval relevance** | Agent self-reports usefulness (1-5) |
| **Context reduction** | Tokens in vs tokens used |
| **Task completion rate** | Compare with/without RAG |
| **Index freshness** | Age of oldest vector |
| **Query latency** | p50/p95 of semantic_search calls |
| **Storage overhead** | Index size vs repo size ratio |

**Instrumentation:**
- Add telemetry to `semantic_search` tool
- Log retrieval results alongside agent actions
- A/B test agents with/without RAG access

---

## Implementation Phases

### Phase 1: Read-only prototype
- [ ] Add lancedb dependency
- [ ] Manual CLI to index a project: `athena index build`
- [ ] Basic semantic_search tool for agents
- [ ] Measure retrieval quality

### Phase 2: Daemon integration
- [ ] Index storage in ~/.athena/indices/
- [ ] Auto-index on daemon startup
- [ ] Incremental updates on file changes
- [ ] TUI status display

### Phase 3: Worktree overlays
- [ ] Per-worktree overlay storage
- [ ] Merge overlay on PR merge
- [ ] Cleanup on worktree removal

### Phase 4: Metrics & optimization
- [ ] Query telemetry
- [ ] Relevance feedback loop
- [ ] Index compression/pruning

---

## Open Questions

1. **Embedding model choice?**
   - OpenAI ada-002 (requires API key, cost)
   - Local model (CPU cost, larger binary)
   - Ollama integration (if user has it)

2. **What to index?**
   - Code files only? Or also docs, configs, tests?
   - Chunk size strategy (functions? classes? files?)
   - Should we index commit messages for history search?

3. **Multi-repo support?**
   - Athena can manage multiple projects
   - Separate indices or unified?

4. **Privacy/security?**
   - Embedding models see your code
   - Local-only option important for some users

---

## References

- [LanceDB Docs](https://lancedb.github.io/lancedb/)
- [RAG patterns](https://www.anthropic.com/research/contextual-retrieval)
- [Code chunking strategies](https://docs.sweep.dev/blogs/chunking-2m-files)
