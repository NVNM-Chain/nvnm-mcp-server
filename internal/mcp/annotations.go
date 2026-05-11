package mcp

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

// Tool-annotation constructors. Each call returns a fresh *ToolAnnotations
// so tool registrations never share an annotation pointer. A shared
// singleton would be mutable shared state across every tool that picked
// the same profile -- a footgun if the SDK or middleware ever mutated
// through the pointer.
//
// Three profiles cover every tool registered by this server:
//
//   - openWorldReadOnly:    reads chain state or other external entities.
//                           Used by evm_get_*, anchor_get_* (except
//                           anchor_info), anchor_prepare_*,
//                           nvnm_setup_wizard, wallet_status.
//   - closedWorldReadOnly:  pure compute or server-local config reads.
//                           Used by nvnm_overview, anchor_info, and the
//                           two verify helpers.
//   - destructiveWriteTool: broadcasts a caller-signed transaction. The
//                           payload may perform destructive operations
//                           (e.g. revokeRole, value transfer);
//                           DestructiveHint is the conservative truth.
//
// MCP spec semantics:
//   - ReadOnlyHint:    true if the tool does not modify state.
//   - DestructiveHint: meaningful only when ReadOnlyHint == false; true if
//                      the tool may perform non-additive updates.
//   - OpenWorldHint:   true if the tool interacts with external entities.
//                      Set explicitly on every tool so reviewers do not
//                      have to infer the spec default.

// newOpenWorldReadOnly returns annotations for a read-only tool that reads
// chain state or other external entities.
func newOpenWorldReadOnly() *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		ReadOnlyHint:  true,
		OpenWorldHint: ptrTrue(),
	}
}

// newClosedWorldReadOnly returns annotations for a read-only tool that
// does not interact with any external entity (pure compute or local
// config reads).
func newClosedWorldReadOnly() *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		ReadOnlyHint:  true,
		OpenWorldHint: ptrFalse(),
	}
}

// newDestructiveWriteTool returns annotations for a tool that may perform
// destructive (non-additive) updates against external state.
func newDestructiveWriteTool() *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: ptrTrue(),
		OpenWorldHint:   ptrTrue(),
	}
}

func ptrTrue() *bool  { v := true; return &v }
func ptrFalse() *bool { v := false; return &v }
