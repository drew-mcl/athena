# WI-C5A6.10: Task List Display Issue - Root Cause Analysis

**Date:** 2026-02-11
**Feature:** Task list integration with `athtree`

## Problem Statement

Task lists weren't showing in `athtree` for work item `wi-c5a6.10`. The tree showed the work item but no task count (no `[X/Y]` indicator), even though tasks were being created in Claude Code.

## Root Cause

### 1. Agent Spawn Method
This agent session was **NOT spawned via `ath spawn`**. Instead, it was likely invoked directly via:
- `claude` command directly
- `claude --resume <session-id>`
- Manual invocation

When agents are spawned via `ath spawn`, Athena sets the environment variable:
```bash
CLAUDE_CODE_TASK_LIST_ID=wi-c5a6.10
```

This tells Claude Code to scope all task operations to the work item ID.

### 2. Task List Location Mismatch
Without `CLAUDE_CODE_TASK_LIST_ID` set, Claude Code defaulted to creating a UUID-based task list directory:
```
~/.claude/tasks/3f0b097f-3a05-405f-9a64-a9d1248577ba/
```

Instead of the expected work-item-scoped directory:
```
~/.claude/tasks/wi-c5a6-10/
```

Note: Claude Code converts dots to hyphens in directory names (`wi-c5a6.10` → `wi-c5a6-10`).

### 3. Sync Mechanism Limitation
Athena's task sync mechanism (in `internal/daemon/work_items.go:369-393`) only watches for task list directories that start with `wi-`:

```go
func (d *Daemon) initialTaskSync() {
    // ...
    for _, l := range lists {
        if !strings.HasPrefix(l.ID, "wi-") {
            continue  // Skips UUID-based lists
        }
        // ...
    }
}
```

Therefore, UUID-based task lists are ignored by the sync mechanism.

## Solution Applied

Manually created the proper task list directory and copied tasks:

```bash
mkdir -p ~/.claude/tasks/wi-c5a6-10
cp ~/.claude/tasks/3f0b097f-3a05-405f-9a64-a9d1248577ba/*.json ~/.claude/tasks/wi-c5a6-10/
```

**What happened next:**
1. The daemon's file watcher (fsnotify) detected the new directory
2. `handleTaskEvent` was triggered with `EventTypeListSync`
3. `syncClaudeTasksToWorkItems` synced the 4 tasks to work items in the database:
   - `wi-c5a6.10.1` - Investigate task list creation for work items (completed)
   - `wi-c5a6.10.2` - Debug athtree task list display logic (completed)
   - `wi-c5a6.10.3` - Fix task list creation or display issue (in_progress)
   - `wi-c5a6.10.4` - Test and commit the fix (pending)
4. `GetWorkItemProgress` now returns `completed=2, total=4`
5. `athtree` displays `[2/4]` next to `wi-c5a6.10`

## Result

**Before:**
```
└─ ◆ wi-c5a6.10   task list still not showing on athtree... ● active
```

**After:**
```
└─ ◆ wi-c5a6.10   task list still not showing on athtree... [2/4] ● active
    ├─ ● wi-c5a6.10.1 Investigate task list creation for work items
    ├─ ● wi-c5a6.10.2 Debug athtree task list display logic
    ├─ ● wi-c5a6.10.3 Fix task list creation or display issue ● active
    └─ ○ wi-c5a6.10.4 Test and commit the fix
```

## Architecture Overview

### Task List Scoping Flow

```
┌─────────────────────────────────────────────────────────────┐
│ 1. User runs: ath spawn wi-c5a6.10                         │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Daemon (spawn.go:40-41)                                  │
│    taskListID := workItem.ID  // "wi-c5a6.10"              │
│    prompt := buildSpawnPrompt(..., taskListID, ...)        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. Agent Spawner (spawner.go:420-421)                       │
│    runSpec.Env["CLAUDE_CODE_TASK_LIST_ID"] = "wi-c5a6.10" │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. Claude Code SDK                                          │
│    Reads CLAUDE_CODE_TASK_LIST_ID env var                  │
│    Scopes all TaskCreate/TaskUpdate/TaskList to:           │
│    ~/.claude/tasks/wi-c5a6-10/                             │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 5. File Watcher (claude/provider.go)                        │
│    fsnotify watches ~/.claude/tasks/wi-c5a6-10/            │
│    Emits TaskEvent on file changes                         │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. Task Sync (work_items.go:396-424)                       │
│    Syncs tasks to work_items table:                        │
│    wi-c5a6.10.1, wi-c5a6.10.2, etc.                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ 7. Tree Display (output.go:321-332)                         │
│    GetWorkItemProgress() counts child work items           │
│    Displays [2/4] in athtree                               │
└─────────────────────────────────────────────────────────────┘
```

### When Spawned Without Athena

```
┌─────────────────────────────────────────────────────────────┐
│ User runs: claude (directly)                                │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ CLAUDE_CODE_TASK_LIST_ID not set                           │
│ Claude Code generates UUID: 3f0b097f-...                   │
│ Creates: ~/.claude/tasks/3f0b097f-.../ ❌                  │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│ Athena's sync mechanism skips UUID-based directories       │
│ No work items created → No task count in athtree ❌        │
└─────────────────────────────────────────────────────────────┘
```

## Recommendations

### Short Term (Document & Validate)
1. **Update CLAUDE.md**: Add prominent warning that agents MUST be spawned via `ath spawn` for proper integration
2. **Add spawn validation**: Log warning when `CLAUDE_CODE_TASK_LIST_ID` is not set or doesn't match work item pattern
3. **Document workaround**: Add section on how to manually link orphaned task lists

### Medium Term (Tooling)
1. **Add `ath task link` command**:
   ```bash
   ath task link <uuid-list-id> <work-item-id>
   # Renames/moves task list to proper location
   ```
2. **Auto-detect orphaned lists**: Scan for UUID-based lists and prompt user to link them
3. **Add spawn detection hook**: Detect when Claude is run directly and offer to link the session

### Long Term (Auto-migration)
1. **Retroactive linking**: Scan all past Claude sessions, correlate with work items by timestamp/path, auto-link
2. **Migration tool**: Provide `ath tasks migrate` to move all orphaned lists
3. **Prevent direct spawns**: Provide `ath claude` wrapper that always sets proper environment

## Code Locations

| Component | File | Lines | Description |
|-----------|------|-------|-------------|
| Task list assignment | `internal/daemon/spawn.go` | 40-41 | Sets taskListID = workItem.ID |
| Environment variable | `internal/agent/spawner.go` | 420-421 | Sets CLAUDE_CODE_TASK_LIST_ID |
| Task sync filter | `internal/daemon/work_items.go` | 382-384 | Filters wi-* prefixes |
| Sync mechanism | `internal/daemon/work_items.go` | 435-466 | Syncs tasks to work_items |
| Progress calculation | `internal/store/work_items.go` | 276-296 | Counts child work items |
| Tree display | `cmd/ath/output.go` | 321-332 | Enriches with progress |

## References

- [Claude Code Task Integration Research](./claude-code-task-integration.md)
- Task Provider: `internal/task/claude/provider.go`
- Task Registry: `internal/task/registry.go`
- Control API: `internal/control/client.go:974-1127`

## Lessons Learned

1. **Environment matters**: Task list scoping relies entirely on environment variables set during spawn
2. **File watching has limits**: The sync mechanism assumes a naming convention (wi-* prefix)
3. **Documentation is critical**: Users need to know that `ath spawn` is the proper way to start agents
4. **Auto-detection could help**: We could detect orphaned task lists and offer to link them
5. **Testing edge cases**: Need tests for "agent spawned outside Athena" scenario
