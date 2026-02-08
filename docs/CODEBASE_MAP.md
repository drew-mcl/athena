# Codebase Map

This document maps high-level system functionality to specific files and symbols in the current CLI-first architecture.

## Core Runtime (Daemon)

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| Daemon lifecycle | `internal/daemon/daemon.go` | `Daemon`, `New`, `Run` | Main background service, signal handling, worker loops. |
| Control API registration | `internal/daemon/daemon.go` | `registerHandlers` | Wires RPC-style methods exposed over the Unix socket. |
| Spawn orchestration | `internal/daemon/spawn.go` | `handleSpawn`, `resolveSpawnTarget`, `buildSpawnPrompt` | Unified agent launch flow for feature/ticket/work-item/bare modes. |
| Merge queue engine | `internal/daemon/merge_queue.go` | `getIntegrationHead`, `refreshQueueGraph`, `cascadeRebase` | Queue head tracking, divergence detection, and rebase cascade. |
| Queue background sync | `internal/daemon/queue_sync.go` | `QueueSync`, `syncProject` | Polls enabled VCS plugins and auto-removes merged queue items. |
| Job execution | `internal/daemon/executor.go` | `JobExecutor`, `ExecuteJob` | Executes planned jobs against agents/worktrees. |

## CLI Surface

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| Main command tree | `cmd/ath/main.go` | `rootCmd`, `spawnCmd`, `pluginCmd`, `interactiveCmd` | User-facing command graph and flag wiring. |
| Command handlers | `cmd/ath/commands.go` | `runSpawn`, `runQueueList`, `runPluginEnable` | Implements CLI behavior via daemon control client. |
| Terminal rendering | `cmd/ath/output.go` | `printStatusBox`, `printQueueTable`, `printWorkItemTree` | Pretty terminal output for work items, queue, agents, worktrees. |
| Interactive browser | `cmd/ath/interactive.go` | `runInteractive` | Keyboard-driven work item/agent navigation. |

## Integrations & Plugins

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| Plugin primitives | `internal/plugin/plugin.go` | `Plugin`, `Registry` | Shared plugin interface and in-memory registry. |
| Plugin config | `internal/plugin/config.go` | `LoadConfig`, `SaveConfig`, `ApplyConfig` | Shared persisted plugin enable/disable state (`~/.config/athena/plugins.json`). |
| GitHub/GitLab VCS plugins | `internal/plugin/vcs/github.go`, `internal/plugin/vcs/gitlab.go` | `GitHub`, `GitLab` | PR and CI status integration via `gh` and `glab`. |
| Linear/Jira PM plugins | `internal/plugin/pm/linear.go`, `internal/plugin/pm/jira.go` | `Linear`, `Jira` | Ticket lookup and issue lifecycle integrations. |

## Data & Domain

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| Control client/server types | `internal/control/client.go`, `internal/control/types.go` | `Client`, `SpawnRequest`, `MergeQueueItemInfo` | RPC request/response contracts used by CLI and daemon. |
| SQLite store | `internal/store/sqlite.go` | `Store`, `New` | Core persistence entrypoint. |
| Work items | `internal/store/work_items.go` | `CreateWorkItem`, `GetWorkItemTree` | Goal/feature/task hierarchy storage. |
| Merge queue storage | `internal/store/merge_queue.go` | `GetQueueHead`, `MarkQueueItemsDiverged` | Queue ordering and integration-head persistence. |
| Worktree storage | `internal/store/worktrees.go` | `ListWorktrees`, `UpsertWorktree` | Worktree metadata and lifecycle state. |

## Agent Execution Layer

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| Agent process spawning | `internal/agent/spawner.go` | `Spawner`, `Spawn` | Launches and supervises Claude Code subprocesses. |
| Runner abstraction | `internal/runner/runner.go` | `Runner`, `Session` | Runner interface for harness implementations. |
| Claude runner | `internal/runner/claude.go` | `ClaudeRunner` | Claude CLI-specific run/stream logic. |
| Task providers | `internal/task/registry.go`, `internal/task/claude/provider.go` | `Registry`, `Provider` | Task list integration layer used by spawn flow. |

## Safety & Platform Utilities

| Feature | File Path | Key Symbols | Description |
|:---|:---|:---|:---|
| Safe subprocess utilities | `internal/executil/executil.go` | `Command`, `SafeEnv` | Guardrails for process execution environment. |
| Logging/Sentry | `internal/logging/logging.go` | `Info`, `Warn`, `CapturePanic` | Structured logs and panic/error reporting hooks. |
| Worktree operations | `internal/worktree/migrate.go`, `internal/worktree/provision.go` | `Migrator`, `Provisioner` | Git worktree creation, validation, and status collection. |
