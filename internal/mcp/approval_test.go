package mcp

import (
	"context"
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

func TestResolveWriteApproval(t *testing.T) {
	tests := []struct {
		name          string
		perClient     string
		globalDefault string
		want          string
	}{
		{"per-client required wins", ApprovalRequired, ApprovalAuto, ApprovalRequired},
		{"per-client auto wins", ApprovalAuto, ApprovalRequired, ApprovalAuto},
		{"empty per-client falls back to global required", "", ApprovalRequired, ApprovalRequired},
		{"empty per-client falls back to global auto", "", ApprovalAuto, ApprovalAuto},
		{"both empty falls back to required", "", "", ApprovalRequired},
		{"invalid per-client falls back to global", "yolo", ApprovalAuto, ApprovalAuto},
		{"invalid both falls back to required", "yolo", "nope", ApprovalRequired},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveWriteApproval(tc.perClient, tc.globalDefault)
			if got != tc.want {
				t.Errorf("ResolveWriteApproval(%q, %q) = %q, want %q",
					tc.perClient, tc.globalDefault, got, tc.want)
			}
		})
	}
}

func TestCheckWriteApproval_AutoSkipsElicitation(t *testing.T) {
	ctx := context.Background()
	err := CheckWriteApproval(ctx, nil, "0xdeadbeef", ApprovalAuto)
	if err != nil {
		t.Fatalf("expected nil error for auto approval, got: %v", err)
	}
}

func TestCheckWriteApproval_PerClientAutoOverridesGlobal(t *testing.T) {
	ctx := auth.ContextWithWriteApproval(context.Background(), ApprovalAuto)
	err := CheckWriteApproval(ctx, nil, "0xdeadbeef", ApprovalRequired)
	if err != nil {
		t.Fatalf("expected nil error when per-client is auto, got: %v", err)
	}
}
