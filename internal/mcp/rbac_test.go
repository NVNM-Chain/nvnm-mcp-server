package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
)

func TestRequireRole_NilClaims_Passes(t *testing.T) {
	ctx := context.Background() // no claims set
	if err := requireRole(ctx, "reader"); err != nil {
		t.Errorf("expected nil for unauthenticated context, got %v", err)
	}
}

func TestRequireRole_EmptyRoles_Passes(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "api-key-client",
		Roles:    nil, // API key with no roles assigned
	})
	if err := requireRole(ctx, "reader"); err != nil {
		t.Errorf("expected nil when claims.Roles is empty, got %v", err)
	}
}

func TestRequireRole_CorrectRole_Passes(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "jwt-client",
		Roles:    []string{"writer"},
	})
	if err := requireRole(ctx, "reader", "writer", "admin"); err != nil {
		t.Errorf("expected nil when caller has required role, got %v", err)
	}
}

func TestRequireRole_WrongRole_Denied(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "jwt-client",
		Roles:    []string{"reader"},
	})
	err := requireRole(ctx, "writer", "admin")
	if err == nil {
		t.Fatal("expected ErrPermissionDenied, got nil")
	}
	if !errors.Is(err, apperrors.ErrPermissionDenied) {
		t.Errorf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestRequireRole_AdminRole_Passes(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "admin-user",
		Roles:    []string{"admin"},
	})
	// admin passes any check that includes "admin" in the allowed set
	for _, required := range [][]string{
		{"writer", "admin"},
		{"admin"},
		{"reader", "writer", "admin", "automation"},
	} {
		if err := requireRole(ctx, required...); err != nil {
			t.Errorf("admin should pass requireRole(%v), got %v", required, err)
		}
	}
	// admin does NOT pass a check that only lists "reader" -- not hierarchical
	if err := requireRole(ctx, "reader"); err == nil {
		t.Error("admin-only role should not pass requireRole('reader') -- not hierarchical")
	}
}

func TestRequireRole_AutomationRole_WriterTools(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "pipeline",
		Roles:    []string{"automation"},
	})
	if err := requireRole(ctx, "writer", "admin", "automation"); err != nil {
		t.Errorf("automation should pass writer/admin/automation check, got %v", err)
	}
}

func TestRequireRole_AutomationRole_GrantRoleDenied(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		ClientID: "pipeline",
		Roles:    []string{"automation"},
	})
	err := requireRole(ctx, "admin") // grant_role requires admin only
	if err == nil {
		t.Fatal("expected ErrPermissionDenied for automation on admin-only tool")
	}
	if !errors.Is(err, apperrors.ErrPermissionDenied) {
		t.Errorf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestRequireRole_PermissionDeniedIsClientSafe(t *testing.T) {
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{
		Roles: []string{"reader"},
	})
	err := requireRole(ctx, "admin")
	safe := apperrors.SafeForClient(err)
	if !errors.Is(safe, apperrors.ErrPermissionDenied) {
		t.Errorf("SafeForClient should pass ErrPermissionDenied through, got %v", safe)
	}
}
