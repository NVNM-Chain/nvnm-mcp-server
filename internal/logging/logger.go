package logging

import (
	"log/slog"
	"os"
	"strings"
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
