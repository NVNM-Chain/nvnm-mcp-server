// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// anchor_get_registry -- a single registry by ID. The narrow counterpart
// to anchor_get_registries: one row, no scan, no paging.
//
// This is the round-trip check for everything setupRegistry wrote: the
// name, description and metadata that went in as calldata come back out
// of the precompile's own storage.

func phaseGetRegistry(t *testing.T, f *flow) {
	var out registryResponse
	f.callOK(t, "anchor_get_registry", map[string]any{"id": f.registryID}, &out)

	if out.ID != f.registryID {
		t.Errorf("id = %d, want %d", out.ID, f.registryID)
	}
	if out.Name != f.registryName {
		t.Errorf("name = %q, want %q", out.Name, f.registryName)
	}
	if out.Description == "" {
		t.Error("description is empty; setupRegistry supplied one")
	}
	if out.CreatedAt == "" {
		t.Error("created_at is empty")
	}
	if out.ContentTrust == "" {
		t.Error("content_trust is empty; a registry name is untrusted on-chain input and must be labelled")
	}

	// The creator is the wallet that signed the addRegistry.
	// docs/TOOL_REFERENCE.md documents this field as "Address of the
	// registry creator (0x-prefixed)", and tells callers resolving a
	// non-unique name to disambiguate on it -- which only works if it is
	// the address they know. The chain stores its own bech32 form, and
	// the server passes the ABI string straight through, so this is where
	// that gap shows up. Asserting the documented contract, not the
	// observed value.
	if !strings.EqualFold(out.Creator, f.address) {
		t.Errorf("creator = %s, want the signing wallet %s; the field is documented as a "+
			"0x-prefixed address and is what a caller disambiguates duplicate registry names on",
			out.Creator, f.address)
	}

	t.Logf("registry %d: name=%q creator=%s created_at=%s", out.ID, out.Name, out.Creator, out.CreatedAt)
}
