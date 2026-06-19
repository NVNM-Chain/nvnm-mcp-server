// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// addTool registers a tool exactly like mcp.AddTool but routes any error the
// handler returns through apperrors.SafeForClient first.
//
// This is the single sanitization choke point for tool errors. The SDK turns
// an error returned from a tool handler into a CallToolResult with
// IsError=true whose content is the raw err.Error() text -- and that result
// flows back to the client WITHOUT passing through the receiving middleware's
// SafeForClient (which only sees protocol-level method errors). Without this
// wrapper a raw upstream error (RPC failures, gas-estimation errors, decode
// errors, internal type paths) would leak verbatim to the client. SafeForClient
// passes known sentinels (not-found, auth, permission, input) through unchanged
// and collapses everything else to a generic upstream-failure message.
func addTool[In, Out any](
	s *mcp.Server,
	t *mcp.Tool,
	h mcp.ToolHandlerFor[In, Out],
) {
	mcp.AddTool(s, t, sanitizeToolErr(h))
}

// sanitizeToolErr wraps a tool handler so its returned error is passed through
// apperrors.SafeForClient before the SDK surfaces it to the client.
//
// IMPORTANT for handler authors: when a handler returns a non-nil error, the
// SDK turns the result into an IsError CallToolResult whose content is ONLY
// the error text -- any structured Out value the handler also returned is
// discarded before it reaches the client. So never return a populated Out
// struct alongside an error expecting the caller to read it (that bug bit the
// nvnm_setup_verify_* tools: their remediation payload was silently dropped).
// If a non-success result is a legitimate outcome the caller must inspect
// (e.g. a verification mismatch with remediation), return it as a SUCCESS
// (nil error) with an ok=false field. Reserve Go errors for "the tool could
// not run" (bad input, upstream failure).
func sanitizeToolErr[In, Out any](
	h mcp.ToolHandlerFor[In, Out],
) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err != nil {
			return res, out, apperrors.SafeForClient(err)
		}
		return res, out, nil
	}
}
