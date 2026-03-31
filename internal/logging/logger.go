package logging

import (
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// New creates a configured slog.Logger based on the given level string.
// Recognized levels: "debug", "info", "warn", "error". Defaults to info.
// Uses JSON output for production readability.
func New(level string) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}
	handler := slog.NewJSONHandler(os.Stderr, opts)
	return slog.New(handler)
}

// NewWithTraceCorrelation creates a JSON logger that automatically injects
// OpenTelemetry trace_id and span_id into every log record when a trace
// context is active. Use after OTel providers are initialized.
func NewWithTraceCorrelation(level, serviceName string) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}
	jsonHandler := slog.NewJSONHandler(os.Stderr, opts)

	otelHandler := otelslog.NewHandler(serviceName,
		otelslog.WithLoggerProvider(nil),
	)

	return slog.New(fanoutHandler{primary: jsonHandler, secondary: otelHandler})
}

// NewText creates a text-formatted logger for local development.
func NewText(level string) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}
	handler := slog.NewTextHandler(os.Stderr, opts)
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
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
