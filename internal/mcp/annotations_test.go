package mcp

import "testing"

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
