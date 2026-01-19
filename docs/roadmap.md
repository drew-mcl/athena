# Roadmap

## Current State (v0.1 - POC)

Core infrastructure for orchestrating Claude Code agents with data sovereignty.

**Completed:**
- [x] Agent lifecycle management (spawn, kill, status tracking)
- [x] Git worktree integration (isolated workspaces per agent)
- [x] TUI dashboard (Bubble Tea)
- [x] Unix socket daemon API
- [x] SQLite persistence
- [x] Control plane types (`internal/spec/`)
- [x] Data plane with event sourcing (`internal/data/`, `internal/eventlog/`)
- [x] Comprehensive CLI option mapping for Claude Code
- [x] Pipeline architecture (EventLog → Cache → Bus → Replicator)

---

## Near Term

### Multi-Harness Support

**Goal:** Run Claude Code and Codex side-by-side, pick the right tool for the job.

- [ ] Codex runner implementation (`internal/runner/codex.go`)
- [ ] Harness capability detection (what can each do?)
- [ ] Harness selection in spawn spec (explicit choice)
- [ ] Unified event normalization across harnesses

### Context Cache Optimization

**Goal:** Sub-second agent restoration after crash or swap.

- [ ] Warm cache on daemon startup (preload recent agents)
- [ ] Snapshot compression (gzip conversation JSON)
- [ ] Cache eviction metrics (hit rate, memory usage)
- [ ] Snapshot persistence to disk (survive daemon restart)

### TUI Improvements

**Goal:** Real-time visibility into agent activity.

- [ ] Wire EventBus to dashboard (live message stream)
- [ ] Message type filtering (show only tool calls, errors, etc.)
- [ ] Conversation replay viewer (step through history)
- [ ] Agent comparison view (side-by-side outputs)
- [ ] All-in-one ops view (Jira/Linear auto creation, CI/CD views, task scheduling/planning, context insights via vector embeddings)

---

## Medium Term

### External Replication

**Goal:** Durable storage beyond local SQLite.

- [ ] MySQL replicator implementation
- [ ] PostgreSQL replicator implementation
- [ ] Async queue with retry logic
- [ ] Replication lag monitoring
- [ ] Conflict resolution strategy

### Cost Tracking

**Goal:** Know what you're spending, set budgets.

- [ ] Token counting per message (input/output)
- [ ] Cost calculation per model tier
- [ ] Per-agent cost aggregation
- [ ] Budget limits with enforcement
- [ ] Cost alerts (approaching limit)

### Multi-Agent Coordination

**Goal:** Agents that spawn and coordinate with other agents.

- [ ] Parent-child agent relationships (already in schema)
- [ ] Result passing between agents
- [ ] Dependency graphs (agent B waits for agent A)
- [ ] Parallel execution with fan-out/fan-in

---

## Long Term

### Context Systems

**Goal:** Agents with memory across sessions.

- [ ] Conversation summarization (compress old context)
- [ ] Vector embedding of messages
- [ ] RAG integration for relevant history retrieval
- [ ] Cross-project memory (patterns, preferences)

### Replay Debugging

**Goal:** Step through agent sessions like a debugger.

- [ ] Deterministic replay from event log
- [ ] Breakpoints on message types
- [ ] State inspection at any point
- [ ] "What if" branching (replay with modified input)

### Auto-Routing

**Goal:** Automatically pick the best harness for each task.

- [ ] Task classification (planning vs execution vs review)
- [ ] Harness strength profiling
- [ ] A/B testing framework
- [ ] Feedback loop for routing improvements

### IDE Integration

**Goal:** Athena in your editor.

- [ ] VS Code extension
- [ ] Neovim plugin
- [ ] JetBrains plugin
- [ ] LSP-style protocol for editor-agnostic support

---

## Non-Goals

Things we're explicitly **not** building:

- **Another harness**: We orchestrate harnesses, we don't compete with them
- **Model fine-tuning**: Use the models as they ship
- **Cloud service**: This runs on your machine, your data stays local
- **Prompt marketplace**: Your prompts, your archetypes, your business

---

## Contributing

Priority is determined by:
1. What unblocks the most valuable workflows
2. What strengthens the core principles (data sovereignty, harness agnosticism)
3. What the community asks for

File issues or PRs at the repo. Tag with `roadmap` to discuss priorities.
