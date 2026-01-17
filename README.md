# Athena

Kubernetes-like orchestration platform for Claude Code agents.

## Overview

Athena manages Claude Code agent lifecycles, worktrees, and provides a TUI dashboard for monitoring and interacting with your AI-assisted development workflow.

**Core metaphor:**
- **Agent = Pod** - isolated execution unit with lifecycle
- **Worktree = Node** - execution environment for agents
- **Context = Volume** - persistent state across restarts
- **Athena = Control Plane** - scheduling, health, orchestration

## Installation

```bash
make install
```

This installs three binaries:
- `athena` - TUI dashboard
- `athenad` - Background daemon
- `wt` - Worktree management

## Quick Start

```bash
# Start everything (daemon + TUI)
make dev

# Or with launchd (macOS, persistent)
make launchd-install
athena
```

## Usage

### athena (TUI Dashboard)

```bash
athena              # Launch TUI (requires daemon running)
athena view <id>    # View live agent output
```

### wt (Worktree Management)

```bash
wt                  # List all worktrees
wt list             # Same as above
wt add <project> <name>  # Create worktree
wt remove <path>    # Remove worktree
wt prune [project]  # Clean up merged worktrees
```

### athenad (Daemon)

The daemon runs in the background managing agent lifecycles. Start it with:

```bash
athenad             # Run in foreground
make launchd-install  # Install as macOS service
```

## Development

```bash
make dev      # Build, start daemon, launch TUI
make stop     # Stop development daemon
make daemon   # Run daemon in foreground (for debugging)
make build    # Build all binaries
make test     # Run tests
```

## Neovim Live Reload

For live reload when agents edit files, add this to your neovim config:

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

terminal:
  provider: ghostty

daemon:
  socket: /tmp/athena.sock
  database: ~/.local/share/athena/athena.db
```

## TUI Keybindings

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

## License

MIT
