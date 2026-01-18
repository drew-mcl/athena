# Development Guide

## Building

```bash
go build ./...           # Build all
go build ./cmd/athena    # Build TUI
go build ./cmd/athenad   # Build daemon
go build ./cmd/wt        # Build worktree tool
```

Or use make:

```bash
make build    # Build all binaries to ./bin
make install  # Install to $GOPATH/bin
make test     # Run tests
```

## Running

### Development Mode

```bash
make dev      # Build, start daemon, launch TUI
make stop     # Stop development daemon
make daemon   # Run daemon in foreground (for debugging)
```

### Production (macOS)

```bash
make launchd-install    # Install as macOS service
make launchd-uninstall  # Remove service
```

## Binaries

| Binary | Purpose |
|--------|---------|
| `athena` | TUI dashboard |
| `athenad` | Background daemon (manages agents, worktrees) |
| `wt` | Worktree management CLI |

## Usage

### athena (TUI)

```bash
athena              # Launch TUI (requires daemon running)
athena view <id>    # View live agent output
```

**Keybindings:**

| Key | Action |
|-----|--------|
| `j/k` | Navigate |
| `e` | Open nvim in worktree |
| `s` | Open shell in worktree |
| `a` | Attach to agent session |
| `v` | View agent live output |
| `n` | New job |
| `x` | Kill agent |
| `r` | Refresh |
| `?` | Help |
| `q` | Quit |

### wt (Worktree Management)

```bash
wt                       # List all worktrees
wt add <project> <name>  # Create worktree
wt remove <path>         # Remove worktree
wt prune [project]       # Clean up merged worktrees
```

### athenad (Daemon)

Runs in background managing agent lifecycles.

```bash
athenad             # Run in foreground
athenad --debug     # With debug logging
```

**Logs:**

Daemon logs to file (doesn't interfere with TUI):
```bash
# View logs in real-time
tail -f ~/.local/share/athena/athena.log

# View with syntax highlighting
bat -f ~/.local/share/athena/athena.log

# Log format: timestamp level source msg key=value...
# 2026-01-17T20:22:35.123-05:00 level=INFO source=daemon.go:97 msg="control server listening" socket=/tmp/athena.sock
```

Agent stderr and crash info are stored in the database and viewable via `L` key in the TUI.

## Configuration

Config file: `~/.config/athena/config.yaml`

```yaml
repos:
  base_dirs:
    - ~/repos
    - ~/work

agents:
  restart_policy: on-failure
  max_restarts: 3
  model: sonnet

archetypes:
  planner:
    description: Explores codebase and drafts plans
    permission_mode: plan
    allowed_tools: [Glob, Grep, Read, Task]
  executor:
    description: Implements approved plans
    permission_mode: default
    allowed_tools: [all]

terminal:
  provider: ghostty

daemon:
  socket: /tmp/athena.sock
  database: ~/.local/share/athena/athena.db
  log_file: ~/.local/share/athena/athena.log
```

## Editor Integration

### Neovim Live Reload

For live reload when agents edit files:

```lua
vim.o.autoread = true

vim.api.nvim_create_autocmd({ "FocusGained", "BufEnter", "CursorHold" }, {
  callback = function()
    if vim.fn.getcmdwintype() == "" then
      vim.cmd("checktime")
    end
  end,
})
```

## Project Structure

```
cmd/
  athena/       # TUI client
  athenad/      # Daemon
  wt/           # Worktree CLI
internal/
  agent/        # Agent lifecycle management
  config/       # Configuration loading
  control/      # Unix socket API
  daemon/       # Core daemon logic
  data/         # Data plane (Message, Conversation)
  eventlog/     # Event sourcing, caching, pub/sub
  runner/       # Harness abstraction (Claude, Codex)
  spec/         # Agent specifications (control plane)
  store/        # SQLite persistence
  tui/          # Bubble Tea components
  worktree/     # Git worktree management
pkg/
  claudecode/   # Claude Code CLI wrapper
```
