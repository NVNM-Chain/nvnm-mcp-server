// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func TestRequireRole(t *testing.T) {
	tests := []struct {
		name    string
		claims  *auth.Claims // nil => no claims in context
		require []string
		wantErr bool
	}{
		{"no identity (stdio/anon) is allowed", nil, []string{"writer"}, false},
		{"authenticated with matching role is allowed",
			&auth.Claims{ClientID: "c1", Roles: []string{"writer"}}, []string{"writer", "admin"}, false},
		{"authenticated with zero roles is DENIED",
			&auth.Claims{ClientID: "c2", Roles: nil}, []string{"writer"}, true},
		{"authenticated with non-matching role is DENIED",
			&auth.Claims{ClientID: "c3", Roles: []string{"reader"}}, []string{"writer", "admin"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tCtx := context.Background()
			if tt.claims != nil {
				tCtx = auth.ContextWithClaims(tCtx, tt.claims)
			}
			err := requireRole(tCtx, tt.require...)
			if tt.wantErr {
				if !errors.Is(err, apperrors.ErrPermissionDenied) {
					t.Fatalf("want ErrPermissionDenied, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

func TestRequireRole_AdminRole_Passes(t *testing.T) {
	tCtx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "admin-user",
		Roles:    []string{"admin"},
	})
	for _, required := range [][]string{
		{"writer", "admin"},
		{"admin"},
		{"reader", "writer", "admin", "automation"},
	} {
		if err := requireRole(tCtx, required...); err != nil {
			t.Errorf("admin should pass requireRole(%v), got %v", required, err)
		}
	}
	if err := requireRole(tCtx, "reader"); err == nil {
		t.Error("admin-only role should not pass requireRole('reader') -- not hierarchical")
	}
}

func TestRequireRole_AutomationRole_WriterTools(t *testing.T) {
	tCtx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "pipeline",
		Roles:    []string{"automation"},
	})
	if err := requireRole(tCtx, "writer", "admin", "automation"); err != nil {
		t.Errorf("automation should pass writer/admin/automation check, got %v", err)
	}
}

func TestRequireRole_AutomationRole_GrantRoleDenied(t *testing.T) {
	tCtx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "pipeline",
		Roles:    []string{"automation"},
	})
	err := requireRole(tCtx, "admin")
	if err == nil {
		t.Fatal("expected ErrPermissionDenied for automation on admin-only tool")
	}
	if !errors.Is(err, apperrors.ErrPermissionDenied) {
		t.Errorf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestRequireRole_PermissionDeniedIsClientSafe(t *testing.T) {
	tCtx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		Roles: []string{"reader"},
	})
	err := requireRole(tCtx, "admin")
	safe := apperrors.SafeForClient(err)
	if !errors.Is(safe, apperrors.ErrPermissionDenied) {
		t.Errorf("SafeForClient should pass ErrPermissionDenied through, got %v", safe)
	}
}
