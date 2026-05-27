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

// requireRole returns nil if the caller holds at least one of the given roles,
// or if no role enforcement is active (no claims, or claims with an empty
// Roles slice -- e.g. stdio transport, unauthenticated HTTP, API key without
// assigned roles).
//
// When roles are present and none match, it returns ErrPermissionDenied so the
// caller can surface a clear, safe error to the MCP client.
func requireRole(ctx context.Context, roles ...string) error {
	c := auth.ClaimsFromContext(ctx)
	if c == nil || len(c.Roles) == 0 {
		return nil
	}
	if c.HasAnyRole(roles...) {
		return nil
	}
	return fmt.Errorf("requires role %s: %w",
		strings.Join(roles, "|"), apperrors.ErrPermissionDenied)
}
