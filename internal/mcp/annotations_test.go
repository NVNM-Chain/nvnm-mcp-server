package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewOpenWorldReadOnly(t *testing.T) {
	a := newOpenWorldReadOnly()
	if a == nil {
		t.Fatal("got nil annotations")
	}
	if !a.ReadOnlyHint {
		t.Error("ReadOnlyHint = false, want true")
	}
	if a.OpenWorldHint == nil || !*a.OpenWorldHint {
		t.Errorf("OpenWorldHint = %v, want explicit true", a.OpenWorldHint)
	}
	if a.DestructiveHint != nil {
		t.Errorf("DestructiveHint = %v, want unset (read-only tools don't set it)", a.DestructiveHint)
	}
}

func TestNewClosedWorldReadOnly(t *testing.T) {
	a := newClosedWorldReadOnly()
	if a == nil {
		t.Fatal("got nil annotations")
	}
	if !a.ReadOnlyHint {
		t.Error("ReadOnlyHint = false, want true")
	}
	if a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("OpenWorldHint = %v, want explicit false", a.OpenWorldHint)
	}
	if a.DestructiveHint != nil {
		t.Errorf("DestructiveHint = %v, want unset", a.DestructiveHint)
	}
}

func TestNewDestructiveWriteTool(t *testing.T) {
	a := newDestructiveWriteTool()
	if a == nil {
		t.Fatal("got nil annotations")
	}
	if a.ReadOnlyHint {
		t.Error("ReadOnlyHint = true, want false (write tool)")
	}
	if a.DestructiveHint == nil || !*a.DestructiveHint {
		t.Errorf("DestructiveHint = %v, want explicit true", a.DestructiveHint)
	}
	if a.OpenWorldHint == nil || !*a.OpenWorldHint {
		t.Errorf("OpenWorldHint = %v, want explicit true", a.OpenWorldHint)
	}
}

func TestConstructorsReturnDistinctPointers(t *testing.T) {
	// Each call must return a fresh struct so tool registrations never
	// share an annotation pointer. Verify the two pointers differ and
	// that mutating one does not affect the other.
	a := newOpenWorldReadOnly()
	b := newOpenWorldReadOnly()
	if a == b {
		t.Fatal("constructors returned the same pointer; expected fresh structs per call")
	}

	*a.OpenWorldHint = false
	if b.OpenWorldHint == nil || !*b.OpenWorldHint {
		t.Errorf("mutating one annotation affected the other; OpenWorldHint=%v", b.OpenWorldHint)
	}
}

// Registration-level regression tests. These assert that every tool the
// server registers carries a complete annotation payload, and that the
// known special-case tools (destructive writer, closed-world reads)
// carry the expected profile.

// expectedToolCount is the number of tools registered when
// enableWriteTools=true on current main. Bump when adding tools.
const expectedToolCount = 16

func TestE2E_AllToolsAreAnnotated(t *testing.T) {
	session := startTestServerWithConfig(t, e2eServerConfig{}, nil)

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if got := len(result.Tools); got != expectedToolCount {
		t.Errorf("tool count = %d, want %d", got, expectedToolCount)
	}

	for _, tool := range result.Tools {
		if tool.Name == "" {
			t.Error("tool has empty Name")
			continue
		}
		if tool.Title == "" {
			t.Errorf("tool %q has empty Title", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty Description", tool.Name)
		}
		if tool.Annotations == nil {
			t.Errorf("tool %q has nil Annotations", tool.Name)
			continue
		}
		if tool.Annotations.OpenWorldHint == nil {
			t.Errorf("tool %q OpenWorldHint is nil; must be set explicitly", tool.Name)
		}
	}
}

func TestE2E_WriteToolIsAnnotatedDestructive(t *testing.T) {
	session := startTestServerWithConfig(t, e2eServerConfig{}, nil)

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	sendRaw := findTool(result.Tools, "evm_send_raw_transaction")
	if sendRaw == nil {
		t.Fatal("evm_send_raw_transaction not registered")
	}

	if sendRaw.Annotations.ReadOnlyHint {
		t.Error("evm_send_raw_transaction ReadOnlyHint = true, want false (write tool)")
	}
	if sendRaw.Annotations.DestructiveHint == nil || !*sendRaw.Annotations.DestructiveHint {
		t.Errorf("evm_send_raw_transaction DestructiveHint = %v, want explicit true",
			sendRaw.Annotations.DestructiveHint)
	}
	if sendRaw.Annotations.OpenWorldHint == nil || !*sendRaw.Annotations.OpenWorldHint {
		t.Errorf("evm_send_raw_transaction OpenWorldHint = %v, want explicit true",
			sendRaw.Annotations.OpenWorldHint)
	}
}

func TestE2E_AnchorInfoIsClosedWorld(t *testing.T) {
	session := startTestServerWithConfig(t, e2eServerConfig{}, nil)

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	info := findTool(result.Tools, "anchor_info")
	if info == nil {
		t.Fatal("anchor_info not registered")
	}

	if !info.Annotations.ReadOnlyHint {
		t.Error("anchor_info ReadOnlyHint = false, want true")
	}
	if info.Annotations.OpenWorldHint == nil || *info.Annotations.OpenWorldHint {
		t.Errorf("anchor_info OpenWorldHint = %v, want explicit false (closed-world: returns local ABI metadata)",
			info.Annotations.OpenWorldHint)
	}
}

func findTool(tools []*mcp.Tool, name string) *mcp.Tool {
	for _, t := range tools {
		if t.Name == name {
			return t
		}
	}
	return nil
}
