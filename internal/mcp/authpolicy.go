// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// ErrAuthRequired is returned by the auth-enforcement middleware when an
// anonymous caller invokes a tool that is not in the keyless-exempt set.
var ErrAuthRequired = errors.New("authentication required")

// authExemptTools lists the tools that may run without authentication
// when MCP_KEYLESS_READS is enabled. A tool's absence means it requires
// auth (fail closed). This set is kept deliberately separate from the
// MCP ReadOnlyHint annotation: ReadOnlyHint is a client-facing hint
// about state mutation, whereas this map is the server-side security
// policy. They coincide today (all 20 ReadOnlyHint==true tools are
// exempt; only evm_send_raw_transaction is gated) but must not be
// silently coupled -- a future read-only-but-sensitive tool should NOT
// auto-join the anonymous surface.
//
// MAINTENANCE: when adding a tool, classify it here. Read/compute/prepare
// tools that expose no per-caller secret -> add as true. Anything that
// broadcasts a transaction or returns caller-private state -> omit (it
// then requires auth by default).
//
// Never add a tool literally named "unknown" to this map. That string is
// the fail-closed sentinel returned by toolNameFromRequest when a tool
// name cannot be extracted; it must remain auth-required.
var authExemptTools = map[string]bool{
	"evm_get_balance":             true,
	"evm_get_block":               true,
	"evm_get_chain_id":            true,
	"evm_get_code":                true,
	"evm_get_logs":                true,
	"evm_get_transaction":         true,
	"evm_get_transaction_receipt": true,
	"evm_call_contract":           true,
	"anchor_get_records":          true,
	"anchor_get_registries":       true,
	"anchor_get_registry":         true,
	"anchor_info":                 true,
	"anchor_prepare_add_record":   true,
	"anchor_prepare_add_registry": true,
	"anchor_prepare_grant_role":   true,
	"nvnm_overview":               true,
	"nvnm_setup_wizard":           true,
	"nvnm_setup_verify_hash":      true,
	"nvnm_setup_verify_signature": true,
	"wallet_status":               true,
}

// RequiresAuth reports whether the named tool must be authenticated.
// Unknown tools require auth (fail closed).
func RequiresAuth(tool string) bool {
	return !authExemptTools[tool]
}

// NewAuthEnforcementMiddleware returns MCP receiving middleware that
// rejects anonymous calls to auth-required tools when keyless reads are
// enabled. When keylessEnabled is false it is a no-op: AuthMiddleware
// guarantees every HTTP request is authenticated, so claims are always
// present. Only tools/call is gated; discovery methods (initialize,
// tools/list, ...) always pass so anonymous clients can find the tools.
func NewAuthEnforcementMiddleware(keylessEnabled bool, logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if !keylessEnabled || method != "tools/call" {
				return next(ctx, method, req)
			}
			tool := toolNameFromRequest(req)
			if RequiresAuth(tool) && auth.ClaimsFromContext(ctx) == nil {
				logger.Warn("rejected anonymous call to auth-required tool",
					slog.String("tool", tool),
				)
				return nil, fmt.Errorf("%w: tool %q", ErrAuthRequired, tool)
			}
			return next(ctx, method, req)
		}
	}
}

// toolNameFromRequest extracts the tool name from a tools/call request,
// mirroring the extraction in internal/telemetry/middleware.go. Returns
// "unknown" when the name cannot be determined — which RequiresAuth
// treats as auth-required (fail closed).
func toolNameFromRequest(req mcp.Request) string {
	type hasName interface{ GetName() string }
	if p, ok := req.GetParams().(hasName); ok {
		return p.GetName()
	}
	if sr, ok := req.(*mcp.ServerRequest[*mcp.CallToolParamsRaw]); ok {
		return sr.Params.Name
	}
	return "unknown"
}
