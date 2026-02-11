# Claude Code Task Integration Research

**Feature:** `wi-c5a6.9` - Can we add task lists to Claude's tasks in the way other agents would?

**Date:** 2026-02-11

## Executive Summary

**Yes, this is already implemented and working!** Athena agents are already integrated with Claude Code's native task system. Each spawned agent gets its own task list scoped to the work item ID via the `CLAUDE_CODE_TASK_LIST_ID` environment variable.

## How Claude Code Task System Works

### Storage Format

Claude Code stores tasks in a file-based format at `~/.claude/tasks/`:

```
~/.claude/tasks/{listID}/
  ├── 1.json          # Task #1
  ├── 2.json          # Task #2
  ├── .lock           # Lock file (agent actively working)
  └── .highwatermark  # Next task ID counter
```

Each JSON file contains a single task object (not wrapped in array):

```json
{
  "id": "1",
  "subject": "Research Claude Code task system",
  "description": "Detailed description...",
  "status": "completed",
  "activeForm": "Researching task system",
  "owner": "",
  "blocks": [],
  "blockedBy": [],
  "metadata": {},
  "createdAt": "2026-02-11T10:00:00Z",
  "updatedAt": "2026-02-11T11:00:00Z"
}
```

### Task Lifecycle

1. **List Creation**: Directory created under `~/.claude/tasks/{listID}`
2. **Task Creation**: New JSON file with auto-incremented numeric ID
3. **Task Updates**: JSON file updated in place
4. **Active Detection**: `.lock` file indicates agent is working on this list
5. **File Watching**: fsnotify watches for filesystem changes and emits events

### Task Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Numeric task ID (e.g., "1", "2") |
| `subject` | string | Brief title (imperative form) |
| `description` | string | Detailed description |
| `status` | string | "pending", "in_progress", "completed" |
| `activeForm` | string | Present continuous form for UI (e.g., "Running tests") |
| `owner` | string | Agent ID if assigned |
| `blocks` | string[] | Task IDs that cannot start until this completes |
| `blockedBy` | string[] | Task IDs that must complete first |
| `metadata` | object | Arbitrary key-value data |
| `createdAt` | string | RFC3339 timestamp |
| `updatedAt` | string | RFC3339 timestamp |

## Athena's Current Integration

### Task List Scoping

Athena sets `CLAUDE_CODE_TASK_LIST_ID` to the **work item ID** when spawning agents:

```go
// internal/agent/spawner.go:420-421
if spec.TaskListID != "" {
    runSpec.Env["CLAUDE_CODE_TASK_LIST_ID"] = spec.TaskListID
}
```

This means:
- Each work item (Goal/Feature/Task) gets its own task list
- Task list ID = work item ID (e.g., `wi-abc.1`)
- Multiple agents on the same work item share the same task list

### Spawn Flow

1. **Work Item Resolution** (`internal/daemon/spawn.go:23-94`)
   - Feature ID → work item lookup
   - Ticket ID → PM plugin fetch → work item creation
   - Bare spawn → anonymous work item creation

2. **Task List Assignment** (`internal/daemon/spawn.go:40-41`)
   ```go
   taskListID := workItem.ID
   prompt := d.buildSpawnPrompt(workItem, parentGoal, ticketContext, taskListID, req.Retrieve)
   ```

3. **Agent Spawn** (`internal/agent/spawner.go:128-252`)
   - Sets `CLAUDE_CODE_TASK_LIST_ID` environment variable
   - Claude Code SDK reads this and scopes all TaskCreate/TaskUpdate/TaskList calls to this list

4. **System Prompt Injection** (`internal/daemon/spawn.go:641-645`)
   ```markdown
   ## Task Tracking

   Your task list ID is `wi-abc.1`. Use Claude Code's task tools (TaskCreate, TaskUpdate, TaskList) to:
   - Break your work into trackable tasks before starting
   - Mark tasks `in_progress` when you start them
   - Mark tasks `completed` when done
   ```

### Provider Architecture

Athena implements a **provider registry pattern** for task backends:

```
task.Registry
  ├── Provider: "claude" (internal/task/claude/provider.go)
  └── Provider: "local" (future: local SQLite storage)
```

**Claude Provider** (`internal/task/claude/provider.go`):
- Implements `task.Provider` interface
- Reads/writes `~/.claude/tasks/` directories
- Watches filesystem for changes via fsnotify
- Caches task lists in memory for performance

**Registry** (`internal/task/registry.go`):
- Multiplexes across providers
- Aggregates task lists from all sources
- Merges event streams via `WatchAll()`

### Daemon API Handlers

Athena daemon exposes task operations via Unix socket (`internal/daemon/tasks.go`):

| Handler | Purpose |
|---------|---------|
| `handleListTaskProviders` | List available providers ("claude") |
| `handleListTaskLists` | List all task lists across providers |
| `handleListTasks` | Get tasks from a specific list |
| `handleGetTask` | Get single task details |
| `handleCreateTask` | Create new task |
| `handleUpdateTask` | Update task fields |
| `handleDeleteTask` | Remove task |
| `handleExecuteTask` | Spawn agent to work on a specific task |
| `handleBroadcastTask` | Send task updates to all connected clients |

These handlers default to `provider="claude"` if not specified.

## Multi-Agent Task Sharing

### Current Behavior

When multiple agents work on the same work item:
- They **share the same task list** (same `CLAUDE_CODE_TASK_LIST_ID`)
- Each agent can see tasks created by others
- Tasks can have `owner` field set to agent ID for assignment
- `.lock` file indicates at least one agent is active

### How It Works

1. **Agent A spawns** on work item `wi-abc.1`
   - Sets `CLAUDE_CODE_TASK_LIST_ID=wi-abc.1`
   - Creates tasks: #1, #2, #3

2. **Agent B spawns** on same work item
   - Also gets `CLAUDE_CODE_TASK_LIST_ID=wi-abc.1`
   - Sees tasks #1, #2, #3 via `TaskList`
   - Can create task #4, update #2 with `owner=AgentB`

3. **Filesystem watching**
   - Claude provider watches `~/.claude/tasks/wi-abc.1/`
   - File changes trigger `EventTypeListSync` events
   - Daemon broadcasts `task_updated` events to connected clients

### Example Use Cases

**Team Coordination:**
```go
// Team lead creates tasks
TaskCreate("Implement API endpoint")
TaskCreate("Add tests")
TaskCreate("Update documentation")

// Spawns teammate on same work item
Task(subagent_type="general-purpose", prompt="Work on task #2")

// Teammate can see and claim tasks
tasks := TaskList()  // Sees all 3 tasks
TaskUpdate(taskId="2", owner="teammate-agent-id", status="in_progress")
```

**Parent-Child Agents:**
```go
// Planner agent on wi-abc.1
TaskCreate("Research authentication patterns")
TaskCreate("Design API schema")
TaskCreate("Implement handlers")

// Executor agent spawned with same work item ID
// Sees tasks created by planner
// Marks them in_progress/completed as work proceeds
```

## Integration Points

### 1. Environment Variable

**Set by:** `internal/agent/spawner.go:420-421`

```go
if spec.TaskListID != "" {
    runSpec.Env["CLAUDE_CODE_TASK_LIST_ID"] = spec.TaskListID
}
```

**Consumed by:** Claude Code SDK automatically scopes task operations to this list ID

### 2. System Prompt

**Built by:** `internal/daemon/spawn.go:641-645`

Injects instructions about task tracking into every agent's system prompt:
- Explains how to use TaskCreate/TaskUpdate/TaskList
- Provides the task list ID
- Sets expectations about task workflow

### 3. CLI Output

**Displayed by:** `cmd/ath/output.go:419-421`

```go
if a.TaskListID != "" {
    fmt.Printf("  %sTask list:%s  %s\n", dim, reset, a.TaskListID)
}
```

Shows task list ID when inspecting agent details via `ath agent <id>`

### 4. Daemon Task API

**Handlers in:** `internal/daemon/tasks.go`

Exposes full CRUD operations over Unix socket for:
- Listing task providers and task lists
- Creating/reading/updating/deleting tasks
- Broadcasting task changes to connected clients
- Executing tasks (spawning agents for specific tasks)

## Feasibility Assessment

### ✅ Already Working

- [x] Task list scoping via `CLAUDE_CODE_TASK_LIST_ID`
- [x] System prompt injection with task instructions
- [x] Provider abstraction for multiple backends
- [x] File-based storage matching Claude's format
- [x] Filesystem watching for live updates
- [x] Multi-agent task sharing on same work item
- [x] Daemon API for task CRUD operations

### 🎯 What Could Be Enhanced

1. **CLI Commands for Task Management**
   - Add `ath tasks list <work-item-id>` to view tasks
   - Add `ath task create/update/delete` commands
   - Show task status in `ath tree` output

2. **TUI Integration**
   - Add "Tasks" tab to dashboard
   - Real-time task updates via event stream
   - Visual task dependency graph

3. **Task Assignment UI**
   - Claim tasks from CLI/TUI
   - Assign tasks to specific agents
   - Show task ownership in agent list

4. **Team Task Coordination**
   - When using TeamCreate, expose shared task list
   - Task ownership and claiming workflow
   - Dependency tracking (blocks/blockedBy)

5. **Cross-Provider Task Sync**
   - Sync tasks to Linear/Jira subtasks
   - Two-way sync with PM tools
   - Export task list as markdown checklist

## Recommendations

### Short Term (Already Working)

**Document existing functionality:**
- Update CLAUDE.md with task list scoping behavior
- Add examples to agent spawn documentation
- Clarify multi-agent task sharing semantics

### Medium Term (Nice to Have)

**Add CLI ergonomics:**
```bash
ath tasks wi-abc.1              # List tasks for work item
ath task create "Fix bug" -w wi-abc.1
ath task update 3 --status=completed
ath task claim 5                # Set owner to current user
```

**Expose in TUI:**
- Tasks panel showing current work item's tasks
- Live updates as agents create/complete tasks
- Dependency visualization

### Long Term (Advanced)

**Team coordination primitives:**
- Task queues for agent teams
- Automatic task assignment based on agent capabilities
- Task templates for common workflows
- Task analytics and burn-down tracking

## Conclusion

**Athena already integrates with Claude Code's task system.** Each agent gets a scoped task list (work item ID), can create/update tasks via native Claude Code tools, and multiple agents can collaborate on the same task list.

The integration is elegant:
- Uses Claude's native task storage format
- Environment variable scoping (no code changes to Claude needed)
- System prompt injection guides agent behavior
- Provider abstraction allows future backends

**For the immediate use case (agents adding tasks that other agents can see):**
- ✅ This works today
- ✅ Set `CLAUDE_CODE_TASK_LIST_ID` to shared work item ID
- ✅ All agents on that work item see the same tasks
- ✅ Use TaskCreate/TaskUpdate/TaskList as documented

**Key Implementation Detail:**
The task list ID is the **work item ID**, not a per-agent ID. This enables task sharing by default when multiple agents work on the same feature/goal.

## References

### Code Locations

- Task provider interface: `internal/task/provider.go`
- Claude provider implementation: `internal/task/claude/provider.go`
- Provider registry: `internal/task/registry.go`
- Daemon task handlers: `internal/daemon/tasks.go`
- Agent spawning with task list: `internal/agent/spawner.go:420-421`
- System prompt injection: `internal/daemon/spawn.go:641-645`
- Control types: `internal/control/client.go:643, 1437`

### Environment Variables

- `CLAUDE_CODE_TASK_LIST_ID` - Scopes task operations to a specific list (set to work item ID)
- `CLAUDE_CODE_ENABLE_TASKS` - Feature flag to enable task system (in user's shell config)

### File Paths

- Task storage: `~/.claude/tasks/{listID}/*.json`
- Lock file: `~/.claude/tasks/{listID}/.lock`
- Highwater mark: `~/.claude/tasks/{listID}/.highwatermark`
