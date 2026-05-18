// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "testing"

// Branch coverage for the next_actions hint builders introduced in Phase
// 8.3. The reachability test in annotations_test.go verifies that every
// Tool: literal references a registered tool; these tests verify that
// each branching helper returns the right hint per branch, so a future
// edit that swaps two branches is caught immediately.

func TestEvmGetReceiptNext_RevertedBranch(t *testing.T) {
	got := evmGetReceiptNext("reverted")
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Tool != "evm_get_transaction" {
		t.Errorf("Tool = %q, want evm_get_transaction (reverted branch should re-inspect the tx)", got[0].Tool)
	}
}

func TestEvmGetReceiptNext_SuccessBranch(t *testing.T) {
	got := evmGetReceiptNext("success")
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Tool != "anchor_get_records" {
		t.Errorf("Tool = %q, want anchor_get_records (success branch should suggest record verification)", got[0].Tool)
	}
}

func TestEvmGetReceiptNext_UnknownStatusFallsToSuccessBranch(t *testing.T) {
	// Any status other than "reverted" takes the non-reverted path.
	// Documents the contract: the helper is permissive, not strict.
	got := evmGetReceiptNext("")
	if len(got) != 1 || got[0].Tool != "anchor_get_records" {
		t.Errorf("empty status: got %+v, want anchor_get_records", got)
	}
}

func TestEvmGetCodeNext_NotContractBranch(t *testing.T) {
	got := evmGetCodeNext(false)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Tool != "evm_get_balance" {
		t.Errorf("Tool = %q, want evm_get_balance (no-bytecode address should redirect to balance)", got[0].Tool)
	}
}

func TestEvmGetCodeNext_IsContractBranch(t *testing.T) {
	got := evmGetCodeNext(true)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Tool != "evm_call_contract" {
		t.Errorf("Tool = %q, want evm_call_contract (contract address should suggest a read call)", got[0].Tool)
	}
}

func TestAnchorGetRegistriesNext_EmptyBranch(t *testing.T) {
	got := anchorGetRegistriesNext(true)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Tool != "anchor_prepare_add_registry" {
		t.Errorf("Tool = %q, want anchor_prepare_add_registry (empty result should suggest creating one)", got[0].Tool)
	}
}

func TestAnchorGetRegistriesNext_NonEmptyBranch(t *testing.T) {
	got := anchorGetRegistriesNext(false)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	wantTools := map[string]bool{"anchor_get_registry": false, "anchor_prepare_add_record": false}
	for _, na := range got {
		if _, ok := wantTools[na.Tool]; !ok {
			t.Errorf("unexpected Tool = %q", na.Tool)
			continue
		}
		wantTools[na.Tool] = true
	}
	for tool, seen := range wantTools {
		if !seen {
			t.Errorf("missing expected Tool = %q", tool)
		}
	}
}

func TestNextActions_NilForToolsWithoutHints(t *testing.T) {
	// Documents the contract for the "intentionally no hint" tools. If
	// either of these starts returning hints, the change should be
	// deliberate -- the test forces a conscious update.
	if got := evmGetBalanceNext(); got != nil {
		t.Errorf("evmGetBalanceNext = %+v, want nil (reserved for Phase 8.8 wallet_status)", got)
	}
	if got := evmCallContractNext(); got != nil {
		t.Errorf("evmCallContractNext = %+v, want nil", got)
	}
}

func TestEvmSendRawTxNext_EchoesTxHashIntoHint(t *testing.T) {
	const txHash = "0xdeadbeef"
	got := evmSendRawTxNext(txHash)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Tool != "evm_get_transaction_receipt" {
		t.Errorf("Tool = %q, want evm_get_transaction_receipt", got[0].Tool)
	}
	if !contains(got[0].Hint, txHash) {
		t.Errorf("Hint = %q, want substring %q (caller-provided tx_hash must round-trip into the hint)", got[0].Hint, txHash)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
