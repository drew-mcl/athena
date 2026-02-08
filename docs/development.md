# Development Guide

## Build

```bash
go build ./...               # Build all packages
go build ./cmd/ath           # Primary CLI
go build ./cmd/athenad       # Daemon
go build ./cmd/athena-cli    # Legacy CLI shim
go build ./cmd/athena-mcp    # MCP server
```

Or use Make:

```bash
make build
make install
make test
```

## Run

### Development mode

```bash
make dev      # Build + start daemon in background
make stop     # Stop development daemon
make daemon   # Run daemon in foreground
```

### Production (macOS)

```bash
make launchd-install
make launchd-uninstall
make launchd-restart
```

## Binaries

| Binary | Purpose |
|--------|---------|
| `ath` | Primary CLI for goals/features/tasks/spawn/queue/plugins |
| `athenad` | Background daemon (worktrees, agents, queue, task sync) |
| `athena-cli` | Legacy standalone CLI compatibility binary |
| `athena-mcp` | MCP server for agent/tool integration |

## CLI Usage

```bash
ath                          # Status summary
ath spawn -f wi-a3f8.1       # Spawn feature agent (primary flow)
ath i                        # Interactive agent in current directory
ath tree                     # Work item hierarchy
ath queue                    # Merge queue
ath plugin                   # Plugin status and management
ath wt                       # Worktree status
```

## Daemon Logs

```bash
tail -f ~/.local/share/athena/athena.log
```

## Configuration

Config file: `~/.config/athena/config.yaml`

```yaml
repos:
  base_dirs:
    - ~/repos

agents:
  restart_policy: on-failure
  max_restarts: 3

archetypes:
  planner:
    permission_mode: plan
  executor:
    permission_mode: default

daemon:
  socket: /tmp/athena.sock
  database: ~/.local/share/athena/athena.db
  log_file: ~/.local/share/athena/athena.log
```

## Project Structure

```text
cmd/
  ath/           # Primary CLI
  athenad/       # Daemon
  athena-cli/    # Legacy CLI shim
  athena-mcp/    # MCP server
internal/
  agent/         # Agent spawning/lifecycle
  config/        # Configuration loading
  control/       # Unix socket API client/server types
  daemon/        # Core orchestration and queue sync
  eventlog/      # Event sourcing and snapshots
  plugin/        # VCS/PM plugin system
  runner/        # Harness abstraction
  store/         # SQLite persistence
  worktree/      # Worktree management
pkg/
  claudecode/    # Claude Code wrapper
```
