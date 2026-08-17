// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// anchor_prepare_add_registry -- the first write tool an agent meets.
//
// That it produces a transaction which actually lands is already proven:
// setupRegistry signed and broadcast one, and every later phase depends
// on the registry it created. What this phase asserts is the *output
// contract* -- the shape of what the tool hands back -- and what it
// refuses. Both are free: preparing is a read, so nothing here costs gas
// or reaches the chain as a write.

func phasePrepareAddRegistry(t *testing.T, f *flow) {
	utx := f.prepare(t, "anchor_prepare_add_registry", map[string]any{
		"from":        f.address,
		"name":        "mcp-e2e-contract-check-" + uniqueSuffix(),
		"description": "prepared but never broadcast",
		"metadata":    `{"suite":"test/e2e"}`,
	})

	assertUnsignedTxShape(t, f, utx)

	// Rejections. Each is validated client-side before any RPC, so a
	// missing required field must come back as a tool error rather than
	// an opaque revert at gas-estimation time.
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"no from", map[string]any{"name": "x", "description": "y"}},
		{"no name", map[string]any{"from": f.address, "description": "y"}},
		{"malformed from", map[string]any{"from": "not-an-address", "name": "x", "description": "y"}},
	} {
		if got, errText := f.tryPrepare(t, "anchor_prepare_add_registry", tc.args); got != nil {
			t.Errorf("%s: the tool returned a transaction instead of rejecting the call", tc.name)
		} else {
			t.Logf("%s: rejected -- %s", tc.name, errText)
		}
	}
}
