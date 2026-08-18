// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// anchor_prepare_revoke_role -- taking back what grant_role handed out.
//
// It runs directly after the grant phase and revokes that same editor
// role, which is what keeps a run from accumulating permissions on the
// testnet. It is also the stricter of the pair to get wrong: a revoke
// that silently does nothing looks identical to one that worked.

func phaseRevokeRole(t *testing.T, f *flow) {
	utx := f.prepare(t, "anchor_prepare_revoke_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"account":     granteeAddress,
		"role":        "editor",
	})
	assertUnsignedTxShape(t, f, utx)

	r := f.signBroadcastConfirm(t, utx)
	t.Logf("revoked editor on registry %d from %s (gas %d)", f.registryID, granteeAddress, r.GasUsed)

	// Revoking a role that is no longer held. The precompile decides
	// whether that is idempotent or an error; both are defensible, and
	// neither is documented, so this records which one it is rather than
	// asserting an answer.
	second, errText := f.tryPrepare(t, "anchor_prepare_revoke_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"account":     granteeAddress,
		"role":        "editor",
	})
	if second == nil {
		t.Logf("revoking an already-revoked role is rejected: %s", errText)
	} else {
		t.Log("revoking an already-revoked role is accepted (idempotent); not broadcast")
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
		if got, rejectText := f.tryPrepare(t, "anchor_prepare_revoke_role", tc.args); got != nil {
			t.Errorf("%s: the tool returned a transaction instead of rejecting the call", tc.name)
		} else {
			t.Logf("%s: rejected -- %s", tc.name, rejectText)
		}
	}
}
