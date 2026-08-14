// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// TestRequireRole_NamesTheCredential guards against the ambiguity that made a
// registry's own admin unable to tell which authorization had failed. The
// anchor role tools grant and revoke ON-CHAIN roles, so a denial reading
// "requires role admin" naturally parses as "the signing address lacks the
// registry role" when it actually means the caller's credential does. Claims
// are unified across API-key and FusionAuth JWT validation, so the message
// must name the credential neutrally rather than claiming an API key exists.
func TestRequireRole_NamesTheCredential(t *testing.T) {
	claimsCtx := auth.ContextWithClaims(ctx, &auth.Claims{ClientID: "k", Roles: []string{"reader", "writer"}})

	err := requireRole(claimsCtx, "admin")
	if err == nil {
		t.Fatal("reader,writer key allowed into an admin-only tool")
	}
	msg := err.Error()
	if !strings.Contains(msg, "credential") {
		t.Errorf("message does not name the caller's credential: %q", msg)
	}
	if !strings.Contains(msg, "server-side") {
		t.Errorf("message does not distinguish server-side RBAC from the on-chain role check: %q", msg)
	}
	if strings.Contains(msg, "your API key") {
		t.Errorf("message misidentifies JWT callers as API-key holders: %q", msg)
	}
	if !strings.Contains(msg, "admin") {
		t.Errorf("message does not name the required role: %q", msg)
	}
}
