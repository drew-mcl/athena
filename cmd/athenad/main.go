// Command athenad is the Athena background daemon.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/daemon"
	"github.com/drewfead/athena/internal/logging"
)

// Version is set at build time
var Version = "dev"

func main() {
	exitCode := run()
	os.Exit(exitCode)
}

func run() (exitCode int) {
	// Top-level panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Try to log to Sentry if initialized
			logging.CapturePanic(r, "component", "main")
			fmt.Fprintf(os.Stderr, "FATAL: unrecovered panic: %v\n", r)
			exitCode = 2
		}
	}()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	// Initialize structured logging with Sentry
	logLevel := parseLogLevel(cfg.Daemon.LogLevel)
	if err := logging.Init(logging.Config{
		Level:     logLevel,
		SentryDSN: cfg.Daemon.SentryDSN,
		Env:       getEnv(),
		Version:   Version,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logging: %v\n", err)
	}
	defer logging.Flush(2 * time.Second)

	d, err := daemon.New(cfg)
	if err != nil {
		logging.Error("failed to initialize daemon", "error", err)
		return 1
	}

	logging.Info("starting athenad",
		"version", Version,
		"socket", cfg.Daemon.Socket,
		"sentry", cfg.Daemon.SentryDSN != "",
	)

	if err := d.Run(); err != nil {
		logging.Error("daemon error", "error", err)
		return 1
	}

	return 0
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getEnv() string {
	if env := os.Getenv("ATHENA_ENV"); env != "" {
		return env
	}
	return "development"
}
