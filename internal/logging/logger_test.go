package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNew_ReturnsJSONLogger(t *testing.T) {
	logger := New("info")
	if logger == nil {
		t.Fatal("New returned nil")
	}
}

func TestNewText_ReturnsTextLogger(t *testing.T) {
	logger := NewText("debug")
	if logger == nil {
		t.Fatal("NewText returned nil")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"trace", slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseLevel(tc.input)
			if got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNew_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	handler := slog.NewJSONHandler(&buf, opts)
	logger := slog.New(handler)

	logger.Info("test message", slog.String("key", "value"))

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if entry["msg"] != "test message" {
		t.Errorf("msg = %v, want %q", entry["msg"], "test message")
	}
	if entry["key"] != "value" {
		t.Errorf("key = %v, want %q", entry["key"], "value")
	}
}

func TestNew_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelWarn}
	handler := slog.NewJSONHandler(&buf, opts)
	logger := slog.New(handler)

	logger.Info("should be filtered")
	if buf.Len() > 0 {
		t.Errorf("info message should be filtered at warn level, got: %s", buf.String())
	}

	logger.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("warn message should appear at warn level")
	}
}
