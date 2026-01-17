.PHONY: all build install clean test lint fmt dev stop help

# Build configuration
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# Go configuration
GOBIN?=$(shell go env GOBIN)
ifeq ($(GOBIN),)
	GOBIN=$(shell go env GOPATH)/bin
endif

# Paths
DAEMON_PID=/tmp/athenad.pid
PLIST_PATH=~/Library/LaunchAgents/com.athena.daemon.plist

all: help

# Build all binaries
build:
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/athena ./cmd/athena
	go build $(LDFLAGS) -o bin/athenad ./cmd/athenad
	go build $(LDFLAGS) -o bin/wt ./cmd/wt

# Install all binaries to GOBIN
install: build
	cp bin/athena $(GOBIN)/athena
	cp bin/athenad $(GOBIN)/athenad
	cp bin/wt $(GOBIN)/wt
	@echo "Installed: athena, athenad, wt"

clean:
	rm -rf bin/
	go clean

test:
	go test -v ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...
	goimports -w .

# Development workflow
dev: build stop
	@mkdir -p ~/.local/share/athena
	@echo "Starting daemon..."
	@./bin/athenad & echo $$! > $(DAEMON_PID)
	@sleep 0.5
	@echo "Launching TUI..."
	@./bin/athena || true
	@$(MAKE) stop

# Stop the development daemon
stop:
	@if [ -f $(DAEMON_PID) ]; then \
		kill $$(cat $(DAEMON_PID)) 2>/dev/null || true; \
		rm -f $(DAEMON_PID); \
		echo "Stopped daemon"; \
	fi

# Run just the daemon in foreground (for debugging)
daemon: build
	./bin/athenad

# Launchd service management (macOS)
launchd-install: install
	@mkdir -p ~/Library/LaunchAgents
	@echo '<?xml version="1.0" encoding="UTF-8"?>' > $(PLIST_PATH)
	@echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' >> $(PLIST_PATH)
	@echo '<plist version="1.0">' >> $(PLIST_PATH)
	@echo '<dict>' >> $(PLIST_PATH)
	@echo '    <key>Label</key>' >> $(PLIST_PATH)
	@echo '    <string>com.athena.daemon</string>' >> $(PLIST_PATH)
	@echo '    <key>ProgramArguments</key>' >> $(PLIST_PATH)
	@echo '    <array>' >> $(PLIST_PATH)
	@echo '        <string>$(GOBIN)/athenad</string>' >> $(PLIST_PATH)
	@echo '    </array>' >> $(PLIST_PATH)
	@echo '    <key>RunAtLoad</key>' >> $(PLIST_PATH)
	@echo '    <true/>' >> $(PLIST_PATH)
	@echo '    <key>KeepAlive</key>' >> $(PLIST_PATH)
	@echo '    <true/>' >> $(PLIST_PATH)
	@echo '    <key>StandardOutPath</key>' >> $(PLIST_PATH)
	@echo '    <string>$(HOME)/.local/share/athena/athenad.stdout.log</string>' >> $(PLIST_PATH)
	@echo '    <key>StandardErrorPath</key>' >> $(PLIST_PATH)
	@echo '    <string>$(HOME)/.local/share/athena/athenad.stderr.log</string>' >> $(PLIST_PATH)
	@echo '</dict>' >> $(PLIST_PATH)
	@echo '</plist>' >> $(PLIST_PATH)
	@launchctl load $(PLIST_PATH)
	@echo "Installed and started launchd service"

launchd-uninstall:
	@launchctl unload $(PLIST_PATH) 2>/dev/null || true
	@rm -f $(PLIST_PATH)
	@echo "Uninstalled launchd service"

launchd-restart:
	@launchctl unload $(PLIST_PATH) 2>/dev/null || true
	@launchctl load $(PLIST_PATH)
	@echo "Restarted launchd service"

# Database management
db-reset:
	@rm -f ~/.local/share/athena/athena.db
	@echo "Database reset. Run 'make dev' to recreate."

db-backup:
	@mkdir -p ~/.local/share/athena/backups
	@cp ~/.local/share/athena/athena.db ~/.local/share/athena/backups/athena-$(shell date +%Y%m%d-%H%M%S).db
	@echo "Backed up database"

schema:
	@sqlite3 ~/.local/share/athena/athena.db ".schema"

# Help
help:
	@echo "Athena - Kubernetes for Claude Code Agents"
	@echo ""
	@echo "Binaries:"
	@echo "  athena   - TUI dashboard for monitoring agents"
	@echo "  athenad  - Background daemon managing agent lifecycles"
	@echo "  wt       - Standalone worktree management tool"
	@echo ""
	@echo "Usage:"
	@echo "  make build     Build all binaries"
	@echo "  make install   Install binaries to GOBIN"
	@echo "  make clean     Remove build artifacts"
	@echo "  make test      Run tests"
	@echo "  make lint      Run linter"
	@echo "  make fmt       Format code"
	@echo ""
	@echo "Development:"
	@echo "  make dev       Build, start daemon, launch TUI"
	@echo "  make stop      Stop development daemon"
	@echo "  make daemon    Run daemon in foreground (for debugging)"
	@echo ""
	@echo "macOS Service:"
	@echo "  make launchd-install     Install and start launchd service"
	@echo "  make launchd-uninstall   Stop and remove launchd service"
	@echo "  make launchd-restart     Restart launchd service"
	@echo ""
	@echo "Database:"
	@echo "  make db-reset    Delete database"
	@echo "  make db-backup   Backup database"
	@echo "  make schema      Show database schema"
