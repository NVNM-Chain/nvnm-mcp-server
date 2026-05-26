// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

func TestRequiresAuth_ExemptReadTools(t *testing.T) {
	exempt := []string{
		"evm_get_balance", "evm_get_block", "evm_get_chain_id", "evm_get_code",
		"evm_get_logs", "evm_get_transaction", "evm_get_transaction_receipt",
		"evm_call_contract",
		"anchor_get_records", "anchor_get_registries", "anchor_get_registry",
		"anchor_info",
		"anchor_prepare_add_record", "anchor_prepare_add_registry", "anchor_prepare_grant_role",
		"nvnm_overview", "nvnm_setup_wizard", "nvnm_setup_verify_hash",
		"nvnm_setup_verify_signature", "wallet_status",
	}
	if len(exempt) != 20 {
		t.Fatalf("expected 20 exempt tools, listed %d", len(exempt))
	}
	for _, tool := range exempt {
		if RequiresAuth(tool) {
			t.Errorf("RequiresAuth(%q) = true, want false (read tool)", tool)
		}
	}
}

func TestRequiresAuth_WriteToolRequiresAuth(t *testing.T) {
	if !RequiresAuth("evm_send_raw_transaction") {
		t.Error("evm_send_raw_transaction must require auth")
	}
}

func TestRequiresAuth_UnknownToolFailsClosed(t *testing.T) {
	if !RequiresAuth("some_future_tool") {
		t.Error("unknown tool must require auth (fail closed)")
	}
}

func TestAuthExemptTools_ExactlyTwenty(t *testing.T) {
	if len(authExemptTools) != 20 {
		t.Errorf("authExemptTools has %d entries, want 20 (one per read tool)", len(authExemptTools))
	}
}

// callToolReq builds a minimal tools/call ServerRequest naming tool.
func callToolReq(tool string) (string, mcp.Request) {
	return "tools/call", &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: tool},
	}
}

func runEnforcement(ctx context.Context, t *testing.T, keyless bool, method string, req mcp.Request) error {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := NewAuthEnforcementMiddleware(keyless, logger)
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})
	_, err := handler(ctx, method, req)
	return err
}

func TestEnforcement_AnonReadToolAllowed(t *testing.T) {
	method, req := callToolReq("evm_get_balance")
	if err := runEnforcement(t.Context(), t, true, method, req); err != nil {
		t.Errorf("anon read tool rejected: %v", err)
	}
}

func TestEnforcement_AnonWriteToolRejected(t *testing.T) {
	method, req := callToolReq("evm_send_raw_transaction")
	if err := runEnforcement(t.Context(), t, true, method, req); !errors.Is(err, ErrAuthRequired) {
		t.Errorf("anon write tool must be rejected with ErrAuthRequired; got err=%v", err)
	}
}

// TestEnforcement_UnrecognizedToolFailsClosed exercises the fail-closed
// guarantee at the middleware boundary (not just the RequiresAuth helper):
// a tools/call whose Name is empty or otherwise unknown must be rejected
// for anonymous callers when keyless is enabled.
func TestEnforcement_UnrecognizedToolFailsClosed(t *testing.T) {
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: ""},
	}
	if err := runEnforcement(t.Context(), t, true, "tools/call", req); !errors.Is(err, ErrAuthRequired) {
		t.Errorf("unrecognized tool name must fail closed; got err=%v", err)
	}
}

func TestEnforcement_AuthedWriteToolAllowed(t *testing.T) {
	authedCtx := auth.ContextWithClaims(t.Context(), &auth.Claims{ClientID: "c1"})
	method, req := callToolReq("evm_send_raw_transaction")
	if err := runEnforcement(authedCtx, t, true, method, req); err != nil {
		t.Errorf("authed write tool rejected: %v", err)
	}
}

func TestEnforcement_FlagOffNoOp(t *testing.T) {
	method, req := callToolReq("evm_send_raw_transaction")
	if err := runEnforcement(t.Context(), t, false, method, req); err != nil {
		t.Errorf("flag-off must be a no-op, got %v", err)
	}
}

func TestEnforcement_NonToolMethodPasses(t *testing.T) {
	// tools/list must work for anonymous clients (tool discovery).
	if err := runEnforcement(t.Context(), t, true, "tools/list", &mcp.ServerRequest[*mcp.ListToolsParams]{}); err != nil {
		t.Errorf("tools/list rejected for anon: %v", err)
	}
}
