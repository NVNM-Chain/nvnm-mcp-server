// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// evm_send_raw_transaction -- the broadcast relay, and the only tool in
// the server annotated as a destructive write.
//
// Its happy path is exercised by every write in this suite: signBroadcastConfirm
// goes through this tool, so by the time this phase runs it has already
// carried a dozen transactions. What is left to assert is what it
// *refuses*, which nothing else covers.
//
// The relay is scoped: the server decodes the signed transaction and
// rejects any destination that is not the anchor precompile. That check
// is the reason this endpoint cannot be used to move funds or poke
// arbitrary contracts, so it is worth an explicit test rather than
// trusting the annotation.

func phaseSendRawTransaction(t *testing.T, f *flow) {
	for _, tc := range []struct {
		name     string
		signedTx string
	}{
		{"not hex", "nonsense"},
		{"0x but not hex", "0xzzzz"},
		{"empty", ""},
		{"hex but not a transaction", "0xdeadbeef"},
	} {
		result := f.call(t, "evm_send_raw_transaction", map[string]any{"signed_tx": tc.signedTx})
		if !result.IsError {
			t.Errorf("%s: the relay accepted %q instead of rejecting it", tc.name, tc.signedTx)
			continue
		}
		t.Logf("%s: rejected -- %s", tc.name, resultText(result))
	}

	// A well-formed, correctly signed transaction whose destination is
	// not the anchor precompile. This is the relay-scope check: it must
	// be refused before broadcast, so the transaction never reaches the
	// chain and costs nothing.
	//
	// The destination is the signing wallet itself -- a zero-value
	// self-transfer, the most harmless transaction that exists. If the
	// relay scope is ever removed and this is broadcast anyway, it moves
	// no funds and only spends its own gas.
	utx := f.prepare(t, "anchor_prepare_add_record", map[string]any{
		"from":          f.address,
		"registry_id":   f.registryID,
		"uri":           "https://example.invalid/nvnm-e2e/relay-scope",
		"checksum":      f.checksum,
		"checksum_algo": "sha256",
		"metadata":      `{"suite":"test/e2e"}`,
	})
	signed := f.signTo(t, utx, f.address)
	result := f.call(t, "evm_send_raw_transaction", map[string]any{"signed_tx": signed})
	if !result.IsError {
		t.Error("the relay broadcast a transaction addressed to an account other than the anchor " +
			"precompile; this endpoint is documented as a scoped anchoring relay, not a " +
			"general-purpose broadcaster")
		return
	}
	t.Logf("non-anchor destination rejected -- %s", resultText(result))
}
