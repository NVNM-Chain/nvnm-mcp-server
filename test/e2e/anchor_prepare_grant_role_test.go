// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// anchor_prepare_grant_role -- handing admin or editor rights over a
// registry, or over one record within it.
//
// The signing wallet created the fixture registry, so it is that
// registry's admin and the grant is authorized on chain. The grant is
// then taken back by anchor_prepare_revoke_role, which runs next, so the
// run leaves access control as it found it.

// granteeAddress receives (and then loses) the editor role. A burn
// address keeps the grant inert: the run hands real authority to nothing
// that can use it.
const granteeAddress = "0x0000000000000000000000000000000000000001"

func phaseGrantRole(t *testing.T, f *flow) {
	utx := f.prepare(t, "anchor_prepare_grant_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"account":     granteeAddress,
		"role":        "editor",
	})
	assertUnsignedTxShape(t, f, utx)

	r := f.signBroadcastConfirm(t, utx)
	t.Logf("granted editor on registry %d to %s (gas %d)", f.registryID, granteeAddress, r.GasUsed)

	// A record-scoped grant. The optional checksum narrows the role to
	// one document, and is normalized the same way addRecord normalizes
	// it, so the 0x-prefixed and bare forms must encode identically --
	// otherwise a grant scoped with one form would not match a record
	// stored under the other.
	bare := f.prepare(t, "anchor_prepare_grant_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"checksum":    f.checksum,
		"account":     granteeAddress,
		"role":        "editor",
	})
	prefixed := f.prepare(t, "anchor_prepare_grant_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"checksum":    "0x" + f.checksum,
		"account":     granteeAddress,
		"role":        "editor",
	})
	if bare.Data != prefixed.Data {
		t.Error("a 0x-prefixed scoping checksum produced different calldata than the bare form; " +
			"a role scoped with one spelling would not match a record stored under the other")
	}

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"no account", map[string]any{
			"from": f.address, "registry_id": f.registryID, "role": "editor",
		}},
		{"no role", map[string]any{
			"from": f.address, "registry_id": f.registryID, "account": granteeAddress,
		}},
		{"malformed account", map[string]any{
			"from": f.address, "registry_id": f.registryID, "account": "not-an-address", "role": "editor",
		}},
	} {
		if got, errText := f.tryPrepare(t, "anchor_prepare_grant_role", tc.args); got != nil {
			t.Errorf("%s: the tool returned a transaction instead of rejecting the call", tc.name)
		} else {
			t.Logf("%s: rejected -- %s", tc.name, errText)
		}
	}
}
