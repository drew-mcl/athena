# CLAUDE.md

Project-specific instructions for Claude Code when working on Athena.

## Project Overview

Athena is a Go-based orchestration platform for Claude Code agents - think Kubernetes, but for AI-assisted development. It manages agent lifecycles, worktrees, and provides a TUI dashboard for monitoring workflows.

**Architecture:**
- `cmd/athena/` - CLI + TUI client
- `cmd/athenad/` - Background daemon
- `internal/control/` - Unix socket API + client
- `internal/daemon/` - Core daemon logic
- `internal/agent/` - Agent lifecycle management
- `internal/tui/` - Bubble Tea TUI components
- `internal/store/` - SQLite persistence layer
- `internal/logging/` - Structured logging with Sentry

## Development Workflow

### Building

```bash
# Build all binaries
go build ./...

# Build specific binary
go build ./cmd/athenad
go build ./cmd/athena

# Run daemon
./athenad

# Run TUI (requires daemon)
./athena
```

### Testing

```bash
go test ./...
```

## Changelog Requirement

**When adding features, fixes, or significant changes:**

1. After completing the work, add a changelog entry:
   ```bash
   athena changelog add "Feature description" -c feature -d "Detailed description" -p athena
   ```

2. Categories:
   - `feature` - New functionality
   - `fix` - Bug fixes
   - `refactor` - Code improvements
   - `docs` - Documentation

3. If daemon is not running, note the changelog entry to add later.

This ensures we track what's been built and can reference it in release notes.

## Code Conventions

### Logging

Use the `internal/logging` package (not `log`):

```go
import "github.com/drewfead/athena/internal/logging"

logging.Info("message", "key", value)
logging.Error("failed", "error", err)
logging.Debug("details", "data", obj)
```

Errors at ERROR level are automatically sent to Sentry if configured.

### Error Handling

- Return errors up the stack, wrap with context
- Use `fmt.Errorf("context: %w", err)` for wrapping
- Log at the point of handling, not at every layer

### Daemon API

Add new handlers in `internal/daemon/daemon.go`:

```go
d.server.Handle("method_name", d.handleMethodName)
```

Add corresponding client methods in `internal/control/client.go`.

### TUI Components

- TUI uses Bubble Tea framework
- Models in `internal/tui/dashboard/model.go`
- Styling with Lip Gloss in styles.go
- Tab-based navigation pattern

## Key Files

For a detailed mapping of functionality to code, see the **[Codebase Map](docs/CODEBASE_MAP.md)**.

| File | Purpose |
|------|---------|
| `internal/daemon/daemon.go` | Core daemon, API handlers |
| `internal/control/client.go` | Client API types and methods |
| `internal/tui/dashboard/model.go` | Main TUI model |
| `internal/store/sqlite.go` | Database schema and init |
| `internal/logging/logging.go` | Structured logging + Sentry |
| `internal/config/config.go` | Configuration schema |

## Signal Handling

The daemon handles:
- `SIGINT`, `SIGTERM` - Graceful shutdown with work draining
- `SIGHUP` - Config reload (hot reload)

All goroutines use `safeGo()` or `safeLoop()` for panic recovery.

## UX Design Principles

### Core User Flows

1. **Quick Question** - Global, fire-and-forget Q&A (replaces googling)
2. **New Feature** - Ticket ID → API pulls details → Sonnet summarizes → Worktree + .todo → Agent starts
3. **Check on Work** - Agents tab shows status, "awaiting" means needs attention
4. **Notes Pipeline** - Capture idea → Sonnet fleshes out → Creates Jira/Linear ticket

### Key Decisions

- **Questions**: Always global scope
- **Worktree naming**: Always `<ticket-id>-<short-description>` from ticket summary
- **Stale worktrees**: Merged branch = closed
- **One agent per worktree**: No multi-agent on same worktree

### Dashboard Tabs

| Tab | Purpose |
|-----|---------|
| Worktrees | All workspaces, launch pad for work |
| Agents | Active workers, what needs attention |
| Questions | Quick Q&A history |
| Notes | Idea capture, promote to features |

## Goal Spawning Workflow

Athena uses an **orchestrator archetype** for goal-level work. Goals are high-level objectives that get broken down into features.

### How It Works

1. **Spawn from a goal**: `ath spawn --goal "Build user authentication system"`
2. **Orchestrator agent activates**: Automatically uses the "orchestrator" archetype (opus model)
3. **Agent workflow**:
   - Analyzes the goal and explores the codebase
   - Breaks down the goal into discrete features
   - Creates Feature work items under the goal
   - Decides: work solo (sequential) or create a team (parallel)
   - If team: spawns teammates on each feature
   - Coordinates implementation and integration

### Orchestrator Archetype

The orchestrator is designed specifically for goal-level work:
- **Model**: opus (needs planning and coordination capability)
- **Workflow**: Analyze → Break down → Evaluate complexity → Execute
- **Solo approach**: < 5 features, sequential work, localized changes
- **Team approach**: 5+ features, parallel work, multiple areas of codebase

### CLI Commands

```bash
ath spawn --goal "Goal description"           # Create and spawn on a goal
ath spawn --work-item wi-abc123               # Spawn on existing goal
ath tree                                      # View goal → features hierarchy
```

### For Orchestrator Agents

When spawned on a goal:
- Use `TaskCreate` to create Feature work items (tasks under the goal)
- If working solo: implement features sequentially
- If creating a team:
  - Use `TeamCreate` to create a coordinated team
  - Use Task tool to spawn teammates on each feature
  - Assign features with `TaskUpdate` (set owner to teammate name)
  - Coordinate work and handle blockers

## Merge Queue Workflow

Athena uses a **merge queue** to coordinate multiple in-flight features. Features develop in parallel but merge sequentially, ensuring clean integration.

### How It Works

1. **All features branch from main**: When you create a worktree, it branches from the default branch (main/master). This enables true parallel development - you can spawn multiple features simultaneously without them chaining off each other.

2. **Auto-added to queue**: Features are automatically added to the merge queue when created. The queue determines merge order, not branching structure.

3. **Sequential merge via rebasing**: Features merge to main in queue order (position 1 first). Before merging, later positions automatically rebase onto earlier ones, creating a clean chain.

4. **Editing earlier features**: If you need to fix a feature that's earlier in the queue:
   - Make your changes
   - Run `ath queue bump` - this updates the queue
   - Athena **automatically rebases** all dependent features onto your changes
   - If conflicts occur, affected features are marked for manual resolution

### CLI Commands

```bash
ath queue              # Show queue status
ath queue add          # Add current worktree to queue
ath queue head         # Show integration HEAD for new features
ath queue bump         # Move to back after edits (auto-rebases dependents)
ath queue rm           # Remove from queue
```

### For Agents

When working in a worktree:
- Your feature is automatically in the queue (added during worktree creation)
- Check your position: `ath queue` shows queue status
- If fixing an earlier feature: make changes, then `ath queue bump` to update it
- If you see `status: conflict` - resolve the merge conflict, then continue

The queue ensures features integrate cleanly in sequence, even though they develop in parallel.

### Key Files

| File | Purpose |
|------|---------|
| `internal/store/merge_queue.go` | Queue persistence and ordering |
| `internal/daemon/merge_queue.go` | Queue API handlers, auto-rebase |
| `cmd/ath/commands.go` | CLI commands (runQueue*) |

## Agent Archetypes

Athena provides specialized archetypes for different types of work. Install them with `ath enable`.

### Built-in Archetypes (Always Available)

| Archetype | Purpose | Model | Use Case |
|-----------|---------|-------|----------|
| `orchestrator` | Goal-level coordination and feature breakdown | opus | Goals (automatic for goal work items) |
| `executor` | Feature implementation and commits | sonnet | Features (automatic for feature work items) |
| `planner` | Exploration and planning without changes | opus | Research, architecture analysis |
| `reviewer` | Code review and quality checks | sonnet | PR reviews, validation |
| `brainstorm` | Interactive ideation and exploration | opus | User collaboration, design discussions |
| `reconciler` | Branch cleanup, queue reconciliation | sonnet | Maintenance tasks |
| `mapper` | Codebase documentation and mapping | sonnet | Documentation generation |

### Claude Code Subagents (Installed by `ath enable`)

These specialized subagents are installed to `.claude/agents/` and work alongside built-in archetypes:

| Archetype | Purpose | Model | When Claude Uses It |
|-----------|---------|-------|---------------------|
| `code-reducer` | Reduce code size, remove duplication, simplify | sonnet | After features to clean up and consolidate |
| `code-reviewer` | Architecture and code quality review | sonnet | Before merging, after code changes |
| `test-coverer` | Add test coverage, identify untested paths | sonnet | When test coverage is lacking |
| `security-reviewer` | Security audit, vulnerability scanning | sonnet | Before merging security-sensitive changes |
| `performance-optimizer` | Performance analysis and optimization | sonnet | When performance issues are identified |
| `doc-generator` | Documentation generation for code and APIs | sonnet | After features, when docs are missing |

### Installation

```bash
ath enable          # Install lifecycle hooks + agent archetypes
ath disable         # Remove hooks only (preserve archetypes)
ath disable --agents # Remove hooks AND archetypes
```

**What gets installed:**
- Lifecycle hooks in `.claude/settings.json` (SessionStart, Stop, SessionEnd)
- Agent archetypes in `.claude/agents/` (markdown files with YAML frontmatter)

**Customization:**
- Archetypes are only written if they don't already exist (preserves your customizations)
- Edit any archetype file in `.claude/agents/` to customize behavior
- Claude Code automatically loads archetypes from `.claude/agents/`

### Usage Examples

```bash
# Spawn with interactive archetype selector
ath spawn -a select     # Interactive menu of all archetypes
ath spawn -a ?          # Alternative syntax

# Spawn with specific archetype
ath spawn -a planner         # Use planner archetype
ath spawn -a code-reviewer   # Use code-reviewer archetype

# Claude uses archetypes automatically based on descriptions
"Review this code for security issues"  # Uses security-reviewer
"Add test coverage for this module"     # Uses test-coverer
"Reduce duplication in these files"     # Uses code-reducer

# Or request a specific archetype explicitly
"Use the code-reviewer subagent to analyze this PR"
"Have the performance-optimizer look at this slow function"
```

**Interactive Selector**: When you use `-a select` or `-a ?`, Athena displays an interactive menu showing:
- All built-in archetypes (orchestrator, executor, planner, etc.)
- All installed archetypes from `.claude/agents/`
- Description and source for each option

Archetypes integrate seamlessly with the orchestrator workflow - the orchestrator can spawn specialized subagents for specific tasks.
