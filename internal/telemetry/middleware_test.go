// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func newTestMetrics(t *testing.T) *Metrics {
	t.Helper()
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})
	m, err := NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	return m
}

func TestNewMCPMiddleware_NotNil(t *testing.T) {
	metrics := newTestMetrics(t)
	mw := NewMCPMiddleware(metrics, testLogger())
	if mw == nil {
		t.Fatal("NewMCPMiddleware returned nil")
	}
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	id := RequestIDFromContext(context.Background())
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestRequestIDFromContext_Set(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestIDKey{}, "test-id-123")
	id := RequestIDFromContext(ctx)
	if id != "test-id-123" {
		t.Errorf("expected %q, got %q", "test-id-123", id)
	}
}

func TestExtractToolName_NonToolCall(t *testing.T) {
	// For non-tools/call methods, the function returns the method itself
	// without touching the request, so nil is safe.
	name := extractToolName("resources/list", nil)
	if name != "resources/list" {
		t.Errorf("expected %q, got %q", "resources/list", name)
	}
}

// parseLogEntry decodes the captured slog JSON log line into a map for
// structural assertions. Mirrors the pattern in internal/logging/logger_test.go.
func parseLogEntry(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()
	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	return entry
}

// TestMCPMiddleware_OmitsClientIDWhenAnonymous verifies that the structured
// log line does not include a client_id attribute when no claims are
// attached to the context. Anonymous keyless reads must leave no per-caller
// identifier in operational logs (the privacy commitment in DATA_HANDLING).
func TestMCPMiddleware_OmitsClientIDWhenAnonymous(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	metrics := newTestMetrics(t)

	mw := NewMCPMiddleware(metrics, logger)
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, err := handler(t.Context(), "tools/call",
		&mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "evm_get_balance"}})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	entry := parseLogEntry(t, &buf)
	if _, present := entry["client_id"]; present {
		t.Errorf("anonymous call log must omit client_id key; got entry: %+v", entry)
	}
}

// TestMCPMiddleware_IncludesClientIDWhenAuthed locks the bidirectional
// contract: when claims ARE present, the log line includes the client_id
// (so a future "always omit" regression doesn't silently strip telemetry
// from authenticated traffic too).
func TestMCPMiddleware_IncludesClientIDWhenAuthed(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	metrics := newTestMetrics(t)

	mw := NewMCPMiddleware(metrics, logger)
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	ctx := auth.ContextWithClaims(t.Context(), &auth.Claims{ClientID: "c-42"})
	_, err := handler(ctx, "tools/call",
		&mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "evm_get_balance"}})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	entry := parseLogEntry(t, &buf)
	if got := entry["client_id"]; got != "c-42" {
		t.Errorf("authenticated call log must record client_id=c-42; got %v in entry: %+v", got, entry)
	}
}
