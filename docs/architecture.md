# Architecture

## Core Principles

### 1. Data Sovereignty

**Every piece of data flowing through Athena belongs to you.**

When you send a prompt to an agent, Athena captures it. When the agent thinks, calls tools, produces output - Athena captures it all. This data persists in your storage (SQLite locally, optionally replicated to your MySQL/Postgres), not locked in a vendor's proprietary session history.

Why this matters:
- **Context continuity**: Agent crashes? Swap harnesses? Your conversation history survives.
- **Auditability**: Full trace of what was asked, what was done, why.
- **Future-proofing**: Build RAG, memory systems, or whatever comes next on YOUR data.
- **No lock-in**: Switch between Claude Code, Codex, or the next thing without losing history.

### 2. Harness Agnosticism

Athena is not a harness. Claude Code and Codex are harnesses - they're the shovels. Athena is the foreman coordinating which shovel to use where.

The harness landscape is racing. New capabilities ship weekly. Athena's job is to:
- Abstract the common operations (spawn, resume, stream events)
- Expose harness-specific capabilities when they matter
- Let you switch harnesses per-task based on their strengths

### 3. Event Sourcing

All agent I/O is an append-only event log. Never mutate, only append. This gives us:
- Perfect audit trail (what happened, when, in what order)
- Easy replay (restart agent, replay events to restore context)
- Snapshots for efficiency (checkpoint + replay from there)
- Eventual consistency for replication (just replay missing events)

---

## Architecture Layers

```
┌─────────────────────────────────────────────────────────────┐
│                         TUI Layer                           │
│  Dashboard, Agent Viewer, Job Management                    │
└─────────────────────────────────────────────────────────────┘
                            │ EventBus (pub/sub)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Control Plane                          │
│  Daemon (orchestration, scheduling, health)                 │
│  ├── AgentSpec (what an agent IS - role, tools, model)      │
│  └── Spawner (lifecycle management)                         │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Runner Layer                           │
│  Harness abstraction (Claude, Codex, future)                │
│  ├── Runner.Start(RunSpec) → Session                        │
│  └── Session.Events() → chan Event                          │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                       Data Plane                            │
│  Event sourcing, caching, persistence                       │
│  ├── EventLog (append-only, snapshots)                      │
│  ├── ContextCache (LRU, fast restore)                       │
│  ├── EventBus (real-time streaming)                         │
│  └── Replicator (async to external DB)                      │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                     Storage Layer                           │
│  SQLite (hot, local) ──async──▶ MySQL/Postgres (redundant)  │
└─────────────────────────────────────────────────────────────┘
```

---

## Data Flow

### Inbound (Prompt → Agent)

```
User prompt
    │
    ▼
Pipeline.Ingest()  ──▶  EventLog.Append()  (persist)
    │                        │
    │                        ▼
    │                   EventBus.Publish()  (notify TUI)
    │                        │
    ▼                        ▼
Runner.SendJSON()       ContextCache.Update()
    │
    ▼
Harness (Claude/Codex)
```

### Outbound (Agent → Response)

```
Harness event stream
    │
    ▼
Session.Events()
    │
    ▼
data.FromEvent()  ──▶  Pipeline.Ingest()  ──▶  EventLog.Append()
                            │                        │
                            ▼                        ▼
                       EventBus.Publish()      Replicator queue
                            │
                            ▼
                       TUI update
```

---

## Key Types

### Control Plane (`internal/spec/`)

```go
// What an agent IS (semantic, harness-agnostic)
type AgentSpec struct {
    Role         string        // "planner", "executor", "reviewer"
    Mode         ExecutionMode // readonly vs readwrite
    Tools        ToolSet       // abstract tool names
    Model        ModelPref     // fast/balanced/quality
    SystemPrompt string
    TaskPrompt   string
}
```

### Data Plane (`internal/data/`)

```go
// Unified I/O unit (bidirectional)
type Message struct {
    ID        string
    AgentID   string
    Direction Direction   // "in" or "out"
    Type      MessageType // prompt, text, tool_call, etc.
    Sequence  int64       // ordering
    Timestamp time.Time
    Text      string
    Tool      *ToolContent
    Error     *ErrorContent
    Raw       json.RawMessage  // original for debugging
}
```

### Runner Layer (`internal/runner/`)

```go
// Harness abstraction
type Runner interface {
    Provider() string
    Capabilities() Capabilities
    Start(ctx context.Context, spec RunSpec) (Session, error)
    Resume(ctx context.Context, spec ResumeSpec) (Session, error)
}

type Session interface {
    ID() string
    Events() <-chan Event
    SendJSON(msg any) error
    Stop() error
}
```

---

## Future Directions

- **Context systems**: Build memory/RAG on top of persisted conversations
- **Multi-agent coordination**: Route tasks to best-fit harness automatically
- **Cost tracking**: Per-agent, per-task budget enforcement across harnesses
- **Replay debugging**: Step through agent sessions like a debugger
