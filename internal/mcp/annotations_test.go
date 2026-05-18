// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
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
// 16 pre-8.8 tools + 5 onboarding tools registered by Phase 8.8.
const expectedToolCount = 21

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

// TestE2E_NextActionTargetsAreRegisteredTools is the static reachability
// guard for the next_actions envelope pattern.
//
// It parses every non-test .go file in this package, collects every
// string literal that appears as the Tool field of a NextAction
// composite literal, then asserts each is a registered tool name. The
// check is static (no runtime call graph), so it catches typos and
// stale references in every branch of every hint builder -- even
// branches no unit test exercises.
//
// When a tool is added or renamed, the registered set updates
// automatically via the live ListTools call; this test will then catch
// any in-source hint that still points at the old name.
func TestE2E_NextActionTargetsAreRegisteredTools(t *testing.T) {
	// 1. Resolve the registered tool set via the MCP wire protocol.
	session := startTestServerWithConfig(t, e2eServerConfig{}, nil)
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	registered := make(map[string]struct{}, len(result.Tools))
	for _, tool := range result.Tools {
		registered[tool.Name] = struct{}{}
	}

	// 2. Walk every non-test .go file in this package and collect
	//    NextAction Tool refs from all of them. Hints used to live
	//    only in next_actions.go; the Phase 8.8 onboarding tools each
	//    carry their own next_actions inline, so the scan has to be
	//    package-wide.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var refs []toolRef
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		refs = append(refs, collectNextActionToolRefs(t, name)...)
		scanned++
	}
	if scanned == 0 {
		t.Fatal("scanned zero .go files -- working dir wrong?")
	}
	if len(refs) == 0 {
		t.Fatal("collected zero Tool: references across the package -- AST walker broken?")
	}

	// 3. Each reference must match a registered tool.
	for _, ref := range refs {
		if _, ok := registered[ref.name]; !ok {
			t.Errorf("%s:%d references unregistered tool %q", ref.file, ref.line, ref.name)
		}
	}
}

type toolRef struct {
	name string
	file string
	line int
}

// collectNextActionToolRefs parses the given file and returns every
// string literal that appears as the Tool field of a composite literal
// that also carries a Hint field. Pattern-matching by field shape
// rather than by declared type lets us find NextAction literals whose
// type is inferred (slice elements like `[]NextAction{ {Tool: "x",
// Hint: "y"} }` have no explicit type tag on the inner literal).
//
// Within this package, NextAction is the only struct with both Tool
// and Hint fields, so this pattern is unambiguous.
func collectNextActionToolRefs(t *testing.T, path string) []toolRef {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var refs []toolRef
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}

		var toolName string
		var toolLine int
		hasHint := false

		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Tool":
				bl, ok := kv.Value.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(bl.Value)
				if err != nil {
					t.Errorf("could not unquote %s: %v", bl.Value, err)
					continue
				}
				toolName = name
				toolLine = fset.Position(bl.Pos()).Line
			case "Hint":
				hasHint = true
			}
		}

		if toolName != "" && hasHint {
			refs = append(refs, toolRef{name: toolName, file: path, line: toolLine})
		}
		return true
	})
	return refs
}
