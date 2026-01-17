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

## System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                         TUI Layer                           │
│  internal/tui/                                              │
│  Dashboard, Agent Viewer, Job Management                    │
└─────────────────────────────────────────────────────────────┘
                            │ EventBus (pub/sub)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Control Plane                          │
│  internal/spec/, internal/agent/, internal/daemon/          │
│  ├── AgentSpec (what an agent IS - role, tools, model)      │
│  ├── Spawner (lifecycle management)                         │
│  └── Daemon (orchestration, scheduling, health)             │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Runner Layer                           │
│  internal/runner/, pkg/claudecode/                          │
│  ├── ClaudeOptions / CodexOptions (comprehensive CLI maps)  │
│  ├── Runner.Start(RunSpec) → Session                        │
│  └── Session.Events() → chan Event                          │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                       Data Plane                            │
│  internal/data/, internal/eventlog/                         │
│  ├── Message (bidirectional I/O unit)                       │
│  ├── Conversation (ordered message sequence)                │
│  ├── Pipeline (orchestrates all components below)           │
│  │   ├── EventLog (append-only, snapshots)                  │
│  │   ├── ContextCache (LRU, fast restore)                   │
│  │   ├── EventBus (real-time streaming)                     │
│  │   └── Replicator (async to external DB)                  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                     Storage Layer                           │
│  internal/store/                                            │
│  SQLite (hot, local) ──async──▶ MySQL/Postgres (redundant)  │
└─────────────────────────────────────────────────────────────┘
```

---

## Package Reference

### Control Plane: `internal/spec/`

Defines **what an agent is** semantically, independent of any harness.

| File | Purpose |
|------|---------|
| `spec.go` | AgentSpec, ExecutionMode, ToolSet, ModelPref |
| `tools.go` | Abstract tool names, harness-specific mappings |
| `translate.go` | AgentSpec → SpawnOptions translation |

```go
// AgentSpec defines agent behavior at a semantic level.
// Archetypes in config produce these; runner translates to CLI options.
type AgentSpec struct {
    Role         string        // "planner", "executor", "reviewer"
    Description  string        // Human-readable purpose
    Mode         ExecutionMode // readonly vs readwrite
    Tools        ToolSet       // abstract tool names
    Model        ModelPref     // fast/balanced/quality + reasoning flag
    SystemPrompt string        // Role context
    TaskPrompt   string        // Specific task
    MaxTurns     int           // Iteration limit
}

type ExecutionMode string
const (
    ModeReadOnly  ExecutionMode = "readonly"  // plan mode, observation only
    ModeReadWrite ExecutionMode = "readwrite" // can modify files
)
```

**Tool Abstraction**: Abstract tool names map to harness-specific tools:

```go
// Abstract (harness-agnostic)     Claude Code      Codex
// ─────────────────────────────   ───────────      ─────
// file.read                       Read             (built-in)
// file.write                      Edit, Write      (built-in)
// file.glob                       Glob             (built-in)
// search.grep                     Grep             (built-in)
// shell.bash                      Bash             (built-in)
// web.fetch                       WebFetch         (not available)
// agent.task                      Task             (not available)
```

---

### Runner Layer: `internal/runner/`

Harness abstraction with comprehensive CLI option mapping.

| File | Purpose |
|------|---------|
| `options.go` | ClaudeOptions (~40 flags), CodexOptions (~20 flags) |
| `runner.go` | Runner interface, Session interface |
| `claude.go` | Claude Code implementation |
| `codex.go` | Codex implementation (planned) |

**ClaudeOptions** - comprehensive mapping of all `claude` CLI flags:

```go
type ClaudeOptions struct {
    // Session
    SessionID    string   // --session-id
    Resume       string   // --resume (session to continue)
    Continue     bool     // --continue (most recent session)

    // Execution
    Print        bool     // --print (non-interactive)
    MaxTurns     int      // --max-turns
    Plan         bool     // -p (plan mode)

    // Model
    Model        string   // --model (haiku/sonnet/opus)

    // Permissions
    PermissionMode      string   // --permission-mode (default/plan/bypassPermissions)
    AllowedTools        []string // --allowedTools
    DisallowedTools     []string // --disallowedTools
    MCPAllowedTools     []string // --mcp-allowed-tools

    // Prompts
    SystemPrompt        string   // --system-prompt
    AppendSystemPrompt  string   // --append-system-prompt
    PermissionPromptTool string  // --permission-prompt-tool

    // Input/Output
    InputFormat  string   // --input-format (text/stream-json)
    OutputFormat string   // --output-format (text/json/stream-json)

    // MCP Servers
    MCPServers   []string // --mcp-config (server configs)

    // ... 30+ more options
}
```

**CodexOptions** - mapping of Codex CLI flags:

```go
type CodexOptions struct {
    // Model
    Model        string   // --model
    Provider     string   // --provider

    // Execution
    FullAuto     bool     // --full-auto (no confirmations)

    // Context
    Images       []string // --image (vision input)

    // ... additional options
}
```

---

### Data Plane: `internal/data/`

Unified model for **all data flowing through agents** - inputs and outputs.

| File | Purpose |
|------|---------|
| `message.go` | Message, Direction, MessageType, ToolContent, ErrorContent |
| `conversation.go` | Conversation helpers, filtering, summarization |
| `adapter.go` | claudecode.Event → Message conversion |

```go
// Direction indicates message flow.
type Direction string
const (
    Inbound  Direction = "in"   // To agent (prompts, follow-ups)
    Outbound Direction = "out"  // From agent (responses, events)
)

// MessageType categorizes the message content.
type MessageType string
const (
    TypePrompt     MessageType = "prompt"       // Initial/follow-up instruction
    TypeThinking   MessageType = "thinking"     // Agent reasoning
    TypeText       MessageType = "text"         // Text output
    TypeToolCall   MessageType = "tool_call"    // Tool invocation
    TypeToolResult MessageType = "tool_result"  // Tool response
    TypeComplete   MessageType = "complete"     // Terminal success
    TypeError      MessageType = "error"        // Terminal error
)

// Message is the unified data unit for agent I/O.
type Message struct {
    ID        string          `json:"id"`
    AgentID   string          `json:"agent_id"`
    Direction Direction       `json:"direction"`
    Type      MessageType     `json:"type"`
    Sequence  int64           `json:"sequence"`
    Timestamp time.Time       `json:"timestamp"`
    Text      string          `json:"text,omitempty"`
    Tool      *ToolContent    `json:"tool,omitempty"`
    Error     *ErrorContent   `json:"error,omitempty"`
    SessionID string          `json:"session_id,omitempty"`
    Raw       json.RawMessage `json:"raw,omitempty"`
}
```

---

### Event Sourcing: `internal/eventlog/`

Append-only event log with caching, pub/sub, and replication.

| File | Purpose |
|------|---------|
| `eventlog.go` | Interfaces: EventLog, ContextCache, EventBus, Replicator, Pipeline |
| `memory.go` | In-memory implementations for testing |
| `sqlite.go` | SQLite-backed implementations for production |

**Pipeline** orchestrates all event sourcing components:

```go
type Pipeline struct {
    Log        EventLog      // Append-only persistence
    Cache      ContextCache  // Fast context restoration
    Bus        EventBus      // Real-time pub/sub
    Replicator Replicator    // Async external replication
}

// Ingest processes a message through the full pipeline:
// 1. Append to EventLog (durable)
// 2. Update ContextCache (fast access)
// 3. Publish to EventBus (real-time)
// 4. Queue for Replicator (redundancy)
func (p *Pipeline) Ingest(ctx context.Context, msg *data.Message) error
```

**EventLog** - append-only with snapshots:

```go
type EventLog interface {
    Append(ctx context.Context, agentID string, msg *data.Message) (int64, error)
    Read(ctx context.Context, agentID string, fromSeq int64, limit int) ([]*data.Message, error)
    ReadAll(ctx context.Context, agentID string) ([]*data.Message, error)
    LastSequence(ctx context.Context, agentID string) (int64, error)
    Snapshot(ctx context.Context, agentID string) (*Snapshot, error)
    RestoreFromSnapshot(ctx context.Context, snap *Snapshot) error
}
```

**ContextCache** - LRU cache for fast agent restore:

```go
type ContextCache interface {
    Get(agentID string) (*CachedContext, bool)
    Put(agentID string, ctx *CachedContext)
    Invalidate(agentID string)
    Update(agentID string, msg *data.Message)  // Incremental update
}
```

**EventBus** - real-time streaming to TUI:

```go
type EventBus interface {
    Publish(agentID string, msg *data.Message)
    Subscribe(agentID string) Subscriber
    SubscribeAll() Subscriber  // All agents
}

type Subscriber interface {
    Events() <-chan *data.Message
    Close()
}
```

---

### Storage: `internal/store/`

SQLite persistence with optional external replication.

| File | Purpose |
|------|---------|
| `sqlite.go` | Database init, schema, core CRUD |
| `messages.go` | Message table CRUD |
| `agents.go` | Agent table CRUD |

**Schema**:

```sql
-- Agents (control plane state)
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    worktree_path TEXT NOT NULL,
    project_name TEXT NOT NULL,
    archetype TEXT NOT NULL,
    status TEXT NOT NULL,
    prompt TEXT,
    pid INTEGER,
    exit_code INTEGER,
    parent_agent_id TEXT REFERENCES agents(id),
    claude_session_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    heartbeat_at DATETIME
);

-- Messages (data plane - event sourced)
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id),
    direction TEXT NOT NULL,        -- 'in' or 'out'
    type TEXT NOT NULL,             -- prompt, thinking, text, tool_call, etc.
    sequence INTEGER NOT NULL,      -- ordering within agent session
    timestamp DATETIME NOT NULL,
    text TEXT,
    tool_name TEXT,
    tool_input TEXT,                -- JSON
    tool_output TEXT,
    error_code TEXT,
    error_message TEXT,
    session_id TEXT,
    raw TEXT,                       -- original JSON for debugging
    UNIQUE(agent_id, sequence)
);

CREATE INDEX idx_messages_agent ON messages(agent_id, sequence);
```

---

## Data Flow

### Inbound (Prompt → Agent)

```
User prompt
    │
    ▼
Spawner.Spawn()
    │
    ├──▶ data.NewPromptMessage()
    │         │
    │         ▼
    │    Pipeline.Ingest()
    │         │
    │         ├──▶ EventLog.Append()     (persist to SQLite)
    │         ├──▶ Cache.Update()        (update LRU cache)
    │         ├──▶ Bus.Publish()         (notify TUI)
    │         └──▶ Replicator.Queue()    (async to MySQL/Postgres)
    │
    └──▶ claudecode.Spawn()  ───▶  Claude Code CLI
```

### Outbound (Agent → Response)

```
Claude Code CLI
    │
    ▼
Process.Events()  (streaming JSON)
    │
    ▼
Spawner.handleEvents()
    │
    ▼
data.FromClaudeEvent()  ───▶  Message
    │
    ▼
Pipeline.Ingest()
    │
    ├──▶ EventLog.Append()     (persist)
    ├──▶ Cache.Update()        (cache)
    ├──▶ Bus.Publish()         (TUI update)
    └──▶ Replicator.Queue()    (redundancy)
```

---

## Agent Lifecycle

```
                    ┌─────────────┐
                    │  SPAWNING   │
                    └──────┬──────┘
                           │ Process started
                           ▼
                    ┌─────────────┐
              ┌─────│   RUNNING   │─────┐
              │     └──────┬──────┘     │
              │            │            │
        thinking      tool_use     error/timeout
              │            │            │
              ▼            ▼            ▼
        ┌──────────┐ ┌───────────┐ ┌─────────┐
        │ PLANNING │ │ EXECUTING │ │ CRASHED │
        └────┬─────┘ └─────┬─────┘ └─────────┘
             │             │
             └──────┬──────┘
                    │ result event
                    ▼
             ┌─────────────┐
             │  COMPLETED  │
             └─────────────┘

             ┌─────────────┐
             │ TERMINATED  │  (user killed)
             └─────────────┘
```

---

## Configuration Integration

Archetypes in `config.yaml` map to AgentSpec:

```yaml
archetypes:
  planner:
    description: Explores codebase and drafts plans
    permission_mode: plan
    model: sonnet
    allowed_tools: [Glob, Grep, Read, Task]
    prompt: |
      You are a planning agent. Analyze the codebase and create
      detailed implementation plans. Do not modify files.

  executor:
    description: Implements approved plans
    permission_mode: default
    model: sonnet
    allowed_tools: [all]
    prompt: |
      You are an execution agent. Implement the provided plan
      carefully, following existing code patterns.
```

Translation flow:
```
config.Archetype
       │
       │ archetype.ToSpec(taskPrompt)
       ▼
spec.AgentSpec
       │
       │ agentSpec.ToSpawnOptions(sessionID, workDir)
       ▼
claudecode.SpawnOptions
       │
       │ claudecode.Spawn()
       ▼
Claude Code CLI process
```

---

## Future Directions

- **Context Systems**: Build memory/RAG on top of persisted conversations
- **Multi-Agent Coordination**: Route tasks to best-fit harness automatically
- **Cost Tracking**: Per-agent, per-task budget enforcement across harnesses
- **Replay Debugging**: Step through agent sessions like a debugger
- **Codex Runner**: Full implementation once Codex CLI stabilizes
- **MySQL Replicator**: Async replication to external database for redundancy
