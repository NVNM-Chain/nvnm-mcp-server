// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// requireRole returns nil when the caller may proceed, or ErrPermissionDenied
// otherwise. It distinguishes two principals that both present zero roles:
//
//   - No identity at all (c == nil): stdio (local-trusted) transport, or an
//     anonymous keyless-read request that the authentication allowlist
//     (authpolicy.go) has already restricted to exempt read/prepare tools.
//     Allowed -- the security decision was made upstream.
//   - An authenticated identity holding none of the required roles, INCLUDING
//     a key with zero roles assigned. Denied (default-deny): a roleless
//     authenticated key authorizes nothing until a role is granted.
func requireRole(ctx context.Context, roles ...string) error {
	c := auth.ClaimsFromContext(ctx)
	if c == nil {
		return nil
	}
	if c.HasAnyRole(roles...) {
		return nil
	}
	// Name the principal explicitly. On the anchor role tools the bare
	// "requires role admin" was genuinely ambiguous: those tools grant and
	// revoke *on-chain* registry roles, so a denial naturally reads as "the
	// signing address lacks the registry role" when it actually means "this
	// API key lacks the role". A registry's own admin could be told it
	// "requires role admin" with no way to tell which authorization failed.
	return fmt.Errorf(
		"your API key does not hold the role this tool requires (needs %s): %w",
		strings.Join(roles, " or "), apperrors.ErrPermissionDenied)
}
