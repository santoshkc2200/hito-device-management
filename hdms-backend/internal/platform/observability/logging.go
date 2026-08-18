// Package observability wires structured logging, OpenTelemetry tracing and
// Prometheus metrics — the three signals an on-call admin needs to answer
// "why did this kiosk request fail" without SSH'ing into the box.
package observability

import (
	"log/slog"
	"os"
)

// NewLogger builds the process-wide slog.Logger: JSON, so log lines are
// machine-parseable by whatever the hospital's IT team already runs.
func NewLogger(level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
