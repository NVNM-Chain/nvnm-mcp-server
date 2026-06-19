// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// TestSanitizeToolErr_CollapsesRawUpstreamError locks the contract that a raw
// error returned from a tool handler -- which the SDK surfaces to the client as
// CallToolResult content, bypassing the receiving middleware's SafeForClient --
// is collapsed to a generic message and does not leak internal detail.
func TestSanitizeToolErr_CollapsesRawUpstreamError(t *testing.T) {
	raw := errors.New(
		"estimate gas: RPC error: -32000 desc = collections: not found: key " +
			"'1' of type github.com/cosmos/gogoproto/mantrachain.anchoring.v1.Registry")
	h := func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, raw
	}

	_, _, err := sanitizeToolErr(h)(context.Background(), nil, struct{}{})
	if err == nil {
		t.Fatal("expected a sanitized error, got nil")
	}
	for _, leak := range []string{"mantrachain", "gogoproto", "RPC error", "-32000"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("sanitized error leaks %q: %v", leak, err)
		}
	}
}

// TestSanitizeToolErr_PassesKnownSentinels confirms that recognized,
// client-safe sentinels survive the wrapper unchanged (so legitimate
// not-found / auth / input errors still reach the caller intact).
func TestSanitizeToolErr_PassesKnownSentinels(t *testing.T) {
	for _, sentinel := range []error{
		apperrors.ErrRegistryNotFound,
		apperrors.ErrTxNotFound,
		apperrors.ErrAuthRequired,
		apperrors.ErrMissingRequired,
		apperrors.ErrPrecompileValidation,
	} {
		h := func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, sentinel
		}
		_, _, err := sanitizeToolErr(h)(context.Background(), nil, struct{}{})
		if !errors.Is(err, sentinel) {
			t.Errorf("sentinel %v must pass through unchanged, got %v", sentinel, err)
		}
	}
}

// TestSanitizeToolErr_NilErrorUnaffected confirms a successful handler call is
// passed through untouched.
func TestSanitizeToolErr_NilErrorUnaffected(t *testing.T) {
	h := func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, nil
	}
	if _, _, err := sanitizeToolErr(h)(context.Background(), nil, struct{}{}); err != nil {
		t.Errorf("nil error must stay nil, got %v", err)
	}
}
