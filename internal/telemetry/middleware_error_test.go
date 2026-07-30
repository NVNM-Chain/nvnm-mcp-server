// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func callToolReq(name string) mcp.Request {
	return &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: name},
	}
}

// TestMCPMiddleware_ErrorPath verifies the error branch: metrics/status are
// recorded and the error crossing the trust boundary is sanitized by
// SafeForClient (an upstream RPC failure must collapse to the generic text).
func TestMCPMiddleware_ErrorPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	metrics := newTestMetrics(t)

	leaky := fmt.Errorf("dial tcp 10.0.0.5:8545: %w", apperrors.ErrUpstreamRPC)
	mw := NewMCPMiddleware(metrics, logger)
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, leaky
	})

	_, err := handler(t.Context(), "tools/call", callToolReq("evm_get_balance"))
	if err == nil {
		t.Fatal("handler returned nil error, want sanitized error")
	}
	if err.Error() != "upstream operation failed" {
		t.Errorf("client-facing error = %q, want %q", err.Error(), "upstream operation failed")
	}
	if errors.Is(err, apperrors.ErrUpstreamRPC) {
		t.Error("sanitized error must not wrap the internal sentinel")
	}

	entry := parseLogEntry(t, &buf)
	if got := entry["status"]; got != "error" {
		t.Errorf("log status = %v, want %q", got, "error")
	}
	if got := entry["tool"]; got != "evm_get_balance" {
		t.Errorf("log tool = %v, want %q", got, "evm_get_balance")
	}
}

// TestMCPMiddleware_InputErrorPassesThrough locks the complementary contract:
// input-validation errors are client-caused and must reach the client intact.
func TestMCPMiddleware_InputErrorPassesThrough(t *testing.T) {
	metrics := newTestMetrics(t)
	mw := NewMCPMiddleware(metrics, testLogger())
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, apperrors.ErrInvalidAddress
	})

	_, err := handler(t.Context(), "tools/call", callToolReq("evm_get_balance"))
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("input error must pass through unchanged; got %v", err)
	}
}

// TestMCPMiddleware_SetsRequestID verifies the middleware attaches a request
// ID that downstream handlers can read from the context.
func TestMCPMiddleware_SetsRequestID(t *testing.T) {
	metrics := newTestMetrics(t)
	mw := NewMCPMiddleware(metrics, testLogger())

	var seenID string
	handler := mw(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		seenID = RequestIDFromContext(ctx)
		return &mcp.CallToolResult{}, nil
	})

	if _, err := handler(t.Context(), "tools/call", callToolReq("evm_get_balance")); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if seenID == "" {
		t.Error("request ID missing from handler context")
	}
}

// namedParams embeds a real params type (Params has unexported methods) and
// adds GetName, exercising extractToolName's hasName interface branch.
type namedParams struct {
	*mcp.CallToolParamsRaw
}

func (namedParams) GetName() string { return "named_tool" }

// wrappedRequest embeds a real request type (Request has unexported methods)
// and overrides GetParams so tests can steer extractToolName's branches.
type wrappedRequest struct {
	*mcp.ServerRequest[*mcp.CallToolParamsRaw]
	params mcp.Params
}

func (r wrappedRequest) GetParams() mcp.Params { return r.params }

func TestExtractToolName_HasNameParams(t *testing.T) {
	req := wrappedRequest{
		ServerRequest: &mcp.ServerRequest[*mcp.CallToolParamsRaw]{},
		params:        namedParams{&mcp.CallToolParamsRaw{}},
	}
	if got := extractToolName("tools/call", req); got != "named_tool" {
		t.Errorf("extractToolName = %q, want %q", got, "named_tool")
	}
}

func TestExtractToolName_CallToolParamsRaw(t *testing.T) {
	if got := extractToolName("tools/call", callToolReq("evm_chain_id")); got != "evm_chain_id" {
		t.Errorf("extractToolName = %q, want %q", got, "evm_chain_id")
	}
}

func TestExtractToolName_Unknown(t *testing.T) {
	// Params without GetName, and the request itself is not a
	// *ServerRequest[*CallToolParamsRaw], so both extraction paths miss.
	req := wrappedRequest{
		ServerRequest: &mcp.ServerRequest[*mcp.CallToolParamsRaw]{},
		params:        &mcp.CallToolParamsRaw{},
	}
	if got := extractToolName("tools/call", req); got != "unknown" {
		t.Errorf("extractToolName = %q, want %q", got, "unknown")
	}
}
