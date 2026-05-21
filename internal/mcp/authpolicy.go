// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

// authExemptTools lists the tools that may run without authentication
// when MCP_KEYLESS_READS is enabled. A tool's absence means it requires
// auth (fail closed). This set is kept deliberately separate from the
// MCP ReadOnlyHint annotation: ReadOnlyHint is a client-facing hint
// about state mutation, whereas this map is the server-side security
// policy. They coincide today (all 20 ReadOnlyHint==true tools are
// exempt; only evm_send_raw_transaction is gated) but must not be
// silently coupled -- a future read-only-but-sensitive tool should NOT
// auto-join the anonymous surface.
//
// MAINTENANCE: when adding a tool, classify it here. Read/compute/prepare
// tools that expose no per-caller secret -> add as true. Anything that
// broadcasts a transaction or returns caller-private state -> omit (it
// then requires auth by default).
var authExemptTools = map[string]bool{
	"evm_get_balance":             true,
	"evm_get_block":               true,
	"evm_get_chain_id":            true,
	"evm_get_code":                true,
	"evm_get_logs":                true,
	"evm_get_transaction":         true,
	"evm_get_transaction_receipt": true,
	"evm_call_contract":           true,
	"anchor_get_records":          true,
	"anchor_get_registries":       true,
	"anchor_get_registry":         true,
	"anchor_info":                 true,
	"anchor_prepare_add_record":   true,
	"anchor_prepare_add_registry": true,
	"anchor_prepare_grant_role":   true,
	"nvnm_overview":               true,
	"nvnm_setup_wizard":           true,
	"nvnm_setup_verify_hash":      true,
	"nvnm_setup_verify_signature": true,
	"wallet_status":               true,
}

// RequiresAuth reports whether the named tool must be authenticated.
// Unknown tools require auth (fail closed).
func RequiresAuth(tool string) bool {
	return !authExemptTools[tool]
}
