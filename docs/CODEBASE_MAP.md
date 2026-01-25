# Codebase Map

This document maps high-level system functionality to specific source files and key symbols. It is designed to help agents and developers quickly locate the code responsible for specific features.

## 🧠 Core Orchestration (Daemon)

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| **Daemon Entrypoint** | `internal/daemon/daemon.go` | `Daemon`, `New`, `Run` | Main service struct, lifecycle management, and signal handling. |
| **Agent Reconciliation** | `internal/daemon/daemon.go` | `reconcileAgents` | Restores agent state on daemon restart (reattaches to processes). |
| **Job Execution** | `internal/daemon/executor.go` | `JobExecutor`, `executeFeature` | Handles high-level flows: `Feature`, `Quick`, `Question`. |
| **API Server** | `internal/control/api.go` | `RegisterHandlers` | Maps Unix socket commands to daemon methods. |
| **Task Registry** | `internal/task/registry.go` | `Registry` | Manages available tasks (e.g., from Claude Code). |

## 🖥️ Terminal UI (TUI)

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| **Main Model** | `internal/tui/dashboard/model.go` | `Model`, `NewModel` | The root Bubble Tea model holding all UI state. |
| **Event Loop** | `internal/tui/dashboard/model.go` | `Update` | Handles keypresses and incoming system messages. |
| **Project View** | `internal/tui/dashboard/view_project.go` | `viewProject` | Renders the project detail screen. |
| **Agent Logs** | `internal/tui/dashboard/view_dashboard.go` | `viewLogs` | Renders the real-time agent log stream. |
| **Styling** | `internal/tui/styles.go` | `Styles` | Lip Gloss definitions for UI theming. |

## 🏃 Agent Runner Layer

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| **Runner Interface** | `internal/runner/runner.go` | `Runner`, `Session` | Abstract interface for any AI agent harness. |
| **Claude Implementation** | `internal/runner/claude.go` | `ClaudeRunner` | Implementation for `claudecode` CLI. |
| **Harness Options** | `internal/runner/options.go` | `ClaudeOptions` | Maps internal config to CLI flags (strict typing). |
| **Process Spawning** | `internal/agent/spawner.go` | `Spawn` | Orchestrates the actual creation of agent processes. |

## 💾 Data & Persistence

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| **SQLite Store** | `internal/store/sqlite.go` | `Store`, `New` | Main database access point. |
| **Event Bus** | `internal/eventlog/eventlog.go` | `EventBus`, `Pipeline` | Pub/Sub system for streaming agent events to TUI. |
| **Message Types** | `internal/data/message.go` | `Message`, `Type` | Unified data structure for all agent I/O. |
| **Worktree State** | `internal/store/worktrees.go` | `GetWorktree`, `ListWorktrees` | DB methods for managing git worktrees. |

## 🛠️ Utilities & Safety

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| **Safe Execution** | `internal/executil/executil.go` | `Command`, `SafeEnv` | Sanitized command execution (PATH cleaning). |
| **Panic Recovery** | `internal/daemon/daemon.go` | `safeGo`, `safeLoop` | Wrappers to prevent daemon crashes from goroutines. |
| **Structured Logging** | `internal/logging/logging.go` | `Info`, `Error` | Standardized logging wrapper (slog + Sentry). |
