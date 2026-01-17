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

## Future Ideas

### Multi-Agent Orchestration

For large features that are too big for a single agent:
- Sub-worktrees with hash suffix feeding into orchestrator agent
- Parent agent coordinates, child agents do focused work
- Core infrastructure work - not MVP scope

This is a significant architectural addition - explore when base system is stable.
