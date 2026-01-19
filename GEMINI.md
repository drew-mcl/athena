# Athena Project Context

## Project Overview

Athena is an **Intelligence Orchestrator** and **Bloomberg Terminal of Engineering**. It serves as a command-and-control platform that coordinates AI coding agents (specifically Claude Code) to maximize developer productivity.

**Key Philosophy:**
*   **Orchestration over Competition:** Athena manages agents ("shovels" like Claude Code) rather than replacing them.
*   **Data Sovereignty:** All prompts, responses, and events are captured and stored locally (SQLite), ensuring you own your context and history, independent of vendor sessions.
*   **Event Sourcing:** All agent I/O is treated as an append-only event log, enabling auditability, replayability, and state reconstruction.

## Architecture

The system is built in **Go** and consists of three main binaries:

*   **`cmd/athena` (TUI):** A terminal user interface built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) for monitoring agents, managing worktrees, and viewing logs.
*   **`cmd/athenad` (Daemon):** A background service that manages the lifecycle of agents, handles persistence, and exposes a Unix socket API.
*   **`cmd/wt` (Worktree Tool):** A standalone utility for managing git worktrees efficiently.

### Key Directories

*   `internal/daemon/`: Core daemon logic and API handlers.
*   `internal/control/`: Unix socket client/server definitions.
*   **`internal/runner/`**: Abstraction layer for running agents (Claude Code implementation).
*   **`pkg/claudecode/`**: Low-level interaction with the Claude Code CLI.
*   `internal/store/`: SQLite persistence layer (`~/.local/share/athena/athena.db`).
*   `internal/tui/`: UI components and views.
*   `internal/logging/`: Structured logging (with Sentry integration).

## Development Workflow

### Building and Running

The project uses a `Makefile` for standard operations.

*   **Build:** `make build` (Outputs binaries to `bin/`)
*   **Install:** `make install` (Installs to `$GOBIN`)
*   **Dev Mode:** `make dev` (Builds, starts `athenad` in background, and launches `athena` TUI)
*   **Stop Daemon:** `make stop`
*   **Clean:** `make clean`

### Testing and Linting

*   **Test:** `make test` (Runs `go test ./...`)
*   **Lint:** `make lint` (Runs `golangci-lint`)
*   **Format:** `make fmt` (Runs `go fmt` and `goimports`)

### Database

*   **Reset:** `make db-reset` (Deletes local DB)
*   **Schema:** `make schema` (Prints SQLite schema)

## Code Conventions

### Style & Structure
*   **Go Version:** 1.24.0+
*   **Logging:** strict usage of `internal/logging` package.
    *   `logging.Info("msg", "key", val)`
    *   Errors at `ERROR` level are automatically reported to Sentry.
*   **Error Handling:**
    *   Return errors up the stack.
    *   Wrap errors with context: `fmt.Errorf("context: %w", err)`.
    *   Log only at the top level or where handled.
*   **Concurrency:** Use `safeGo()` or `safeLoop()` patterns (in daemon) to ensure panic recovery.

### Architecture Patterns
*   **Clean Architecture:** Strict separation between Control Plane (`internal/spec`), Data Plane (`internal/data`), Runner Layer (`internal/runner`), and Storage (`internal/store`).
*   **Event Driven:** The system relies heavily on an event bus (`internal/eventlog`) to propagate state changes from agents to the TUI.

## Contribution Guidelines

*   **Branching:** `main` is protected. Use short-lived feature branches.
*   **Commits:** Follow **Conventional Commits** (`type(scope): subject`).
    *   Types: `feat`, `fix`, `docs`, `refactor`, `chore`, etc.
*   **Changelog:**
    *   Automated via `release-please`.
    *   **Crucial:** For significant user-facing changes, add an in-app changelog entry:
        ```bash
        athena changelog add "Short description" -c feature -d "Detailed description" -p athena
        ```
*   **Git Hooks:** Run `git config core.hooksPath .githooks` to enable local hooks.
