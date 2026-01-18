// Package logging provides structured logging with Sentry integration.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config holds logging configuration.
type Config struct {
	Level     slog.Level
	SentryDSN string
	Env       string // "development", "production"
	Version   string
	LogFile   string // Path to log file (empty = stderr)
}

// Logger wraps slog.Logger with Sentry integration.
type Logger struct {
	*slog.Logger
	sentryEnabled bool
	logFile       *os.File // nil if logging to stderr
}

var defaultLogger *Logger

// Init initializes the global logger with the given config.
func Init(cfg Config) error {
	// Initialize Sentry if DSN provided
	sentryEnabled := false
	if cfg.SentryDSN != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn:              cfg.SentryDSN,
			Environment:      cfg.Env,
			Release:          cfg.Version,
			TracesSampleRate: 0.1, // Sample 10% of transactions
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				// Could filter or modify events here
				return event
			},
		})
		if err != nil {
			return fmt.Errorf("sentry init: %w", err)
		}
		sentryEnabled = true
	}

	// Determine output destination
	var output io.Writer = os.Stderr
	var logFile *os.File

	if cfg.LogFile != "" {
		// Ensure directory exists
		dir := filepath.Dir(cfg.LogFile)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create log dir: %w", err)
		}

		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		output = f
		logFile = f
	}

	// Create slog handler with our custom handler that sends errors to Sentry
	handler := &sentryHandler{
		Handler: slog.NewTextHandler(output, &slog.HandlerOptions{
			Level:     cfg.Level,
			AddSource: true,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				// Use local timezone and consistent format
				if a.Key == slog.TimeKey {
					if t, ok := a.Value.Any().(time.Time); ok {
						a.Value = slog.StringValue(t.Local().Format("2006-01-02T15:04:05.000-07:00"))
					}
				}
				return a
			},
		}),
		sentryEnabled: sentryEnabled,
	}

	defaultLogger = &Logger{
		Logger:        slog.New(handler),
		sentryEnabled: sentryEnabled,
		logFile:       logFile,
	}

	// Also set as default slog logger
	slog.SetDefault(defaultLogger.Logger)

	return nil
}

// Flush flushes any buffered events to Sentry and closes the log file. Call before shutdown.
func Flush(timeout time.Duration) {
	if defaultLogger != nil {
		if defaultLogger.sentryEnabled {
			sentry.Flush(timeout)
		}
		if defaultLogger.logFile != nil {
			defaultLogger.logFile.Sync()
			defaultLogger.logFile.Close()
		}
	}
}

// Default returns the default logger.
func Default() *Logger {
	if defaultLogger == nil {
		// Return a basic logger if not initialized
		return &Logger{
			Logger:        slog.Default(),
			sentryEnabled: false,
		}
	}
	return defaultLogger
}

// sentryHandler wraps an slog.Handler and sends errors to Sentry.
type sentryHandler struct {
	slog.Handler
	sentryEnabled bool
}

func (h *sentryHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always log via the underlying handler
	if err := h.Handler.Handle(ctx, r); err != nil {
		return err
	}

	// Send errors and above to Sentry
	if h.sentryEnabled && r.Level >= slog.LevelError {
		h.sendToSentry(ctx, r)
	}

	return nil
}

func (h *sentryHandler) sendToSentry(_ context.Context, r slog.Record) {
	event := sentry.NewEvent()
	event.Level = slogLevelToSentry(r.Level)
	event.Message = r.Message
	event.Timestamp = r.Time

	// Add attributes as extra data
	r.Attrs(func(a slog.Attr) bool {
		event.Extra[a.Key] = a.Value.Any()
		return true
	})

	// Add source location if available
	if r.PC != 0 {
		frames := runtime.CallersFrames([]uintptr{r.PC})
		frame, _ := frames.Next()
		event.Exception = []sentry.Exception{{
			Type:  "LogError",
			Value: r.Message,
			Stacktrace: &sentry.Stacktrace{
				Frames: []sentry.Frame{{
					Filename: frame.File,
					Function: frame.Function,
					Lineno:   frame.Line,
				}},
			},
		}}
	}

	sentry.CaptureEvent(event)
}

func (h *sentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sentryHandler{
		Handler:       h.Handler.WithAttrs(attrs),
		sentryEnabled: h.sentryEnabled,
	}
}

func (h *sentryHandler) WithGroup(name string) slog.Handler {
	return &sentryHandler{
		Handler:       h.Handler.WithGroup(name),
		sentryEnabled: h.sentryEnabled,
	}
}

func slogLevelToSentry(level slog.Level) sentry.Level {
	switch {
	case level >= slog.LevelError:
		return sentry.LevelError
	case level >= slog.LevelWarn:
		return sentry.LevelWarning
	case level >= slog.LevelInfo:
		return sentry.LevelInfo
	default:
		return sentry.LevelDebug
	}
}

// Convenience functions that use the default logger

// Debug logs at debug level.
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

// Info logs at info level.
func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

// Warn logs at warn level.
func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

// Error logs at error level and sends to Sentry.
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

// With returns a logger with the given attributes.
func With(args ...any) *slog.Logger {
	return Default().With(args...)
}

// CaptureError sends an error to Sentry with additional context.
func CaptureError(err error, ctx ...any) {
	if defaultLogger != nil && defaultLogger.sentryEnabled {
		sentry.WithScope(func(scope *sentry.Scope) {
			// Add context as tags
			for i := 0; i < len(ctx)-1; i += 2 {
				if key, ok := ctx[i].(string); ok {
					scope.SetExtra(key, ctx[i+1])
				}
			}
			sentry.CaptureException(err)
		})
	}
	// Also log it
	args := append([]any{"error", err}, ctx...)
	Default().Error("captured error", args...)
}

// CaptureMessage sends a message to Sentry.
func CaptureMessage(msg string, level sentry.Level) {
	if defaultLogger != nil && defaultLogger.sentryEnabled {
		sentry.CaptureMessage(msg)
	}
}

// CapturePanic captures a panic value and sends it to Sentry.
// It should be called from a recover() handler.
// Returns the panic value for re-panicking if desired.
func CapturePanic(panicValue any, ctx ...any) any {
	if panicValue == nil {
		return nil
	}

	// Build error message
	msg := fmt.Sprintf("panic: %v", panicValue)

	// Log locally
	args := append([]any{"panic", panicValue}, ctx...)
	Default().Error(msg, args...)

	// Send to Sentry if enabled
	if defaultLogger != nil && defaultLogger.sentryEnabled {
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetLevel(sentry.LevelFatal)
			scope.SetTag("type", "panic")

			// Add context as extras
			for i := 0; i < len(ctx)-1; i += 2 {
				if key, ok := ctx[i].(string); ok {
					scope.SetExtra(key, ctx[i+1])
				}
			}

			// Capture as exception for better stack traces
			if err, ok := panicValue.(error); ok {
				sentry.CaptureException(err)
			} else {
				sentry.CaptureMessage(msg)
			}
		})

		// Flush immediately since we might crash
		sentry.Flush(2 * time.Second)
	}

	return panicValue
}
