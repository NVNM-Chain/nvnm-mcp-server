// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "testing"

func TestRequiresAuth_ExemptReadTools(t *testing.T) {
	exempt := []string{
		"evm_get_balance", "evm_get_block", "evm_get_chain_id", "evm_get_code",
		"evm_get_logs", "evm_get_transaction", "evm_get_transaction_receipt",
		"evm_call_contract",
		"anchor_get_records", "anchor_get_registries", "anchor_get_registry",
		"anchor_info",
		"anchor_prepare_add_record", "anchor_prepare_add_registry", "anchor_prepare_grant_role",
		"nvnm_overview", "nvnm_setup_wizard", "nvnm_setup_verify_hash",
		"nvnm_setup_verify_signature", "wallet_status",
	}
	if len(exempt) != 20 {
		t.Fatalf("expected 20 exempt tools, listed %d", len(exempt))
	}
	for _, tool := range exempt {
		if RequiresAuth(tool) {
			t.Errorf("RequiresAuth(%q) = true, want false (read tool)", tool)
		}
	}
}

func TestRequiresAuth_WriteToolRequiresAuth(t *testing.T) {
	if !RequiresAuth("evm_send_raw_transaction") {
		t.Error("evm_send_raw_transaction must require auth")
	}
}

func TestRequiresAuth_UnknownToolFailsClosed(t *testing.T) {
	if !RequiresAuth("some_future_tool") {
		t.Error("unknown tool must require auth (fail closed)")
	}
}

func TestAuthExemptTools_ExactlyTwenty(t *testing.T) {
	if len(authExemptTools) != 20 {
		t.Errorf("authExemptTools has %d entries, want 20 (one per read tool)", len(authExemptTools))
	}
}
