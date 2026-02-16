# GitHub Copilot Instructions for Athena

## Project Overview

Athena is a Go-based orchestration platform for Claude Code agents - think Kubernetes, but for AI-assisted development. It manages agent lifecycles, worktrees, and provides a TUI dashboard for monitoring workflows.

**Core Concept:** Intelligence orchestrator and "Bloomberg Terminal of Engineering" that coordinates AI coding agents to maximize developer productivity.

**Key Philosophy:**
- **Orchestration over Competition:** Manages agents (like Claude Code) rather than replacing them
- **Data Sovereignty:** All prompts, responses, and events captured locally (SQLite)
- **Event Sourcing:** Agent I/O as append-only event log for auditability

## Architecture

The system consists of three main binaries built in Go 1.24+:

- **`cmd/ath/`** - CLI for work items and agent management
- **`cmd/athenad/`** - Background daemon managing agent lifecycles
- **`cmd/athena-cli/`** - Lightweight CLI client

### Key Directories

- `internal/daemon/` - Core daemon logic and API handlers
- `internal/control/` - Unix socket API + client
- `internal/runner/` - Abstraction layer for running agents
- `internal/store/` - SQLite persistence layer (`~/.local/share/athena/athena.db`)
- `internal/tui/` - Bubble Tea TUI components
- `internal/logging/` - Structured logging with Sentry integration
- `pkg/claudecode/` - Low-level Claude Code CLI interaction

## Navigation

- **[Codebase Map](../docs/CODEBASE_MAP.md)** - Detailed file/symbol index
- **[AGENTS.md](../AGENTS.md)** - General agent guidance
- **[CLAUDE.md](../CLAUDE.md)** - Claude Code specific instructions
- **[GEMINI.md](../GEMINI.md)** - Gemini specific instructions

## Development Workflow

### Building

```bash
make build    # Build all binaries to bin/
make install  # Install to $GOBIN
make dev      # Build, start daemon, and launch TUI
make stop     # Stop development daemon
```

### Testing and Linting

```bash
make test     # Run go test ./...
make lint     # Run golangci-lint
make fmt      # Run go fmt and goimports
```

### Database

```bash
make db-reset   # Delete local database
make db-backup  # Backup database
make schema     # Show SQLite schema
```

## Code Conventions

### Style & Structure

- **Go Version:** 1.24.0+
- **Module:** `github.com/drewfead/athena`
- **Formatting:** Run `make fmt` before committing

### Logging

**MUST** use `internal/logging` package (not `log`):

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

### Concurrency

- Use `safeGo()` or `safeLoop()` patterns (in daemon) for panic recovery
- Daemon handles SIGINT/SIGTERM (graceful shutdown), SIGHUP (config reload)

### Architecture Patterns

- **Clean Architecture:** Separation between Control Plane (`internal/spec`), Data Plane (`internal/data`), Runner Layer (`internal/runner`), Storage (`internal/store`)
- **Event Driven:** Event bus (`internal/eventlog`) propagates state changes from agents to TUI

## Git Workflow

- **`main` is protected** - Do not commit directly
- Work on short-lived feature branches
- Rebase locally to keep 1-3 meaningful commits per PR
- Merge to `main` with rebase/fast-forward (no merge commits)

### Conventional Commits

Format: `type(scope?): subject`

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `build`, `ci`, `revert`

- Use `!` for breaking changes with body explaining impact
- Keep subjects imperative, under ~72 chars
- Examples:
  - `feat(daemon): add auto-merge for queue items`
  - `fix(tui): correct worktree status display`
  - `docs: update installation instructions`

### Git Hooks

Enable repo hooks: `git config core.hooksPath .githooks`

- `pre-commit` - Blocks direct commits on `main` (use `ALLOW_MAIN_COMMIT=1` for emergencies)
- `commit-msg` - Enforces conventional commit format

## Release Management

- Release notes generated from conventional commits via `release-please`
- Workflow: `.github/workflows/release-please.yml`
- Config: `release-please-config.json`, `release-please-manifest.json`
- Merge the release PR to publish and update `CHANGELOG.md`

### Changelog Entries

For significant user-facing changes, add in-app changelog entry:

```bash
athena changelog add "Feature description" -c feature -d "Detailed description" -p athena
```

Categories: `feature`, `fix`, `refactor`, `docs`

## Key Implementation Details

### Daemon API

Add handlers in `internal/daemon/daemon.go`:

```go
d.server.Handle("method_name", d.handleMethodName)
```

Add client methods in `internal/control/client.go`.

### TUI Components

- Framework: Bubble Tea
- Models: `internal/tui/dashboard/model.go`
- Styling: Lip Gloss in `styles.go`
- Pattern: Tab-based navigation

### Merge Queue

Athena uses a merge queue to coordinate multiple features:
- All features branch from `main` (parallel development)
- Auto-added to queue on creation
- Sequential merge via rebasing in queue order
- Commands: `ath queue`, `ath queue add`, `ath queue bump`, `ath queue rm`

## Common Tasks

### Adding a New Daemon Endpoint

1. Define handler method in `internal/daemon/daemon.go`
2. Register in daemon init: `d.server.Handle("endpoint", d.handleEndpoint)`
3. Add client method in `internal/control/client.go`
4. Add types in `internal/control/types.go` if needed

### Adding a New CLI Command

1. Add command in `cmd/ath/commands.go`
2. Use Cobra pattern with `RunE` for error handling
3. Call daemon via control client
4. Follow conventional commit format when committing

### Modifying TUI

1. Locate model in `internal/tui/dashboard/`
2. Update in Bubble Tea pattern (Init/Update/View)
3. Test with `make dev` to see changes in action
4. Use Lip Gloss for styling consistency

## Testing Strategy

- Unit tests: `go test ./...`
- Integration tests: Include daemon and client interaction
- Manual testing: Use `make dev` to test full workflow
- No test for documentation-only changes

## Common Patterns to Follow

1. **Structured Logging:** Always use `internal/logging`, never `log` package
2. **Error Context:** Wrap errors with context as they bubble up
3. **Panic Recovery:** Use `safeGo()` for goroutines in daemon
4. **Event Emission:** Emit events for state changes to update TUI
5. **Clean Shutdown:** Handle signals properly in daemon

## What NOT to Do

- Don't use standard `log` package (use `internal/logging`)
- Don't commit directly to `main`
- Don't forget conventional commit format
- Don't add dependencies without justification
- Don't skip changelog entries for user-facing changes
- Don't ignore linting errors (`make lint`)

## Additional Resources

- Detailed architecture: See `docs/` directory
- Codebase map: `docs/CODEBASE_MAP.md`
- Development guide: `CLAUDE.md` and `AGENTS.md`
- Design docs: Check `docs/` for architectural decisions
