// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// TestE2E_AllTools is the whole suite: one ordered journey through every
// MCP tool the server advertises, against a live deployment and a live
// chain.
//
// It is deliberately a single test with ordered subtests rather than a
// set of independent Test functions. The tools are causally linked -- you
// cannot read a record you have not written, or fetch a receipt for a
// transaction you have not broadcast -- and a real agent meets them in
// this order. Ordered subtests make that dependency explicit and keep the
// on-chain cost to one registry and one record for the whole run.
//
// Each tool's assertions live in its own file, named after the tool
// (anchor_get_records_test.go asserts anchor_get_records, and nothing
// else does). This test is the running order and nothing more: the
// per-tool phases hold the substance, the shared fixture below them holds
// the state, and the coverage phase at the end holds the suite to its
// promise of covering everything the server advertises.
//
// Subtest names match tool names exactly, so a filter reads naturally:
//
//	go test -tags e2e -run 'TestE2E_AllTools/anchor_get_records' ./test/e2e/...
//
// The fixture is built outside any subtest, so it survives that filter.
func TestE2E_AllTools(t *testing.T) {
	f := newFlow(t)

	// What the server says about itself. Everything below reads chain ID,
	// the precompile address, and the tool list from here.
	setupDiscovery(t, f)

	// Tools that need no chain state of ours. They run before the write
	// gate so a keyless or unfunded deployment still gets them checked.
	t.Run("nvnm_overview", func(t *testing.T) { phaseOverview(t, f) })
	t.Run("nvnm_setup_wizard", func(t *testing.T) { phaseSetupWizard(t, f) })
	t.Run("nvnm_setup_verify_hash", func(t *testing.T) { phaseVerifyHash(t, f) })
	t.Run("nvnm_setup_verify_signature", func(t *testing.T) { phaseVerifySignature(t, f) })
	t.Run("wallet_status", func(t *testing.T) { phaseWalletStatus(t, f) })
	t.Run("anchor_info", func(t *testing.T) { phaseAnchorInfo(t, f) })
	t.Run("evm_get_chain_id", func(t *testing.T) { phaseChainID(t, f) })
	t.Run("evm_get_balance", func(t *testing.T) { phaseGetBalance(t, f) })
	t.Run("evm_get_code", func(t *testing.T) { phaseGetCode(t, f) })
	t.Run("evm_call_contract", func(t *testing.T) { phaseCallContract(t, f) })

	// Everything below writes to the chain. Both preconditions are
	// checked here, on the parent: a t.Skip inside a subtest would leave
	// t.Run reporting success and the run would continue into writes that
	// cannot work. Skipping here also suppresses the coverage phase,
	// which would otherwise report a false gap.
	if !f.writeToolsAvailable {
		t.Skip("server does not advertise the write tools; this suite requires the write path")
	}
	if !f.walletFunded {
		t.Skipf("signing wallet %s is unfunded; fund it before running the e2e suite", f.address)
	}

	// The shared fixture every phase below asserts against.
	setupRegistry(t, f)
	setupRecord(t, f)

	// Anchor reads, against the registry and record just created.
	t.Run("anchor_get_registries", func(t *testing.T) { phaseGetRegistries(t, f) })
	t.Run("anchor_get_registry", func(t *testing.T) { phaseGetRegistry(t, f) })
	t.Run("anchor_get_records", func(t *testing.T) { phaseGetRecords(t, f) })

	// The prepare tools' own output contracts. The fixture already proved
	// add_registry and add_record produce a transaction that lands; these
	// assert the shape of what they hand back, and what they refuse.
	t.Run("anchor_prepare_add_registry", func(t *testing.T) { phasePrepareAddRegistry(t, f) })
	t.Run("anchor_prepare_add_record", func(t *testing.T) { phasePrepareAddRecord(t, f) })
	t.Run("evm_send_raw_transaction", func(t *testing.T) { phaseSendRawTransaction(t, f) })

	// Mutate the record's status and prove the change stuck on chain,
	// then the same tool against a record with several versions -- the
	// only place its index parameter selects anything.
	t.Run("anchor_prepare_update_record_status", func(t *testing.T) { phaseUpdateRecordStatus(t, f) })
	t.Run("anchor_prepare_update_record_status_versioned", func(t *testing.T) {
		phaseUpdateRecordStatusVersioned(t, f)
	})

	// Registry access control.
	t.Run("anchor_prepare_grant_role", func(t *testing.T) { phaseGrantRole(t, f) })
	t.Run("anchor_prepare_revoke_role", func(t *testing.T) { phaseRevokeRole(t, f) })

	// EVM reads, pointed at the transaction and block this run produced,
	// so they assert against data with a known expected shape.
	t.Run("evm_get_transaction", func(t *testing.T) { phaseGetTransaction(t, f) })
	t.Run("evm_get_transaction_receipt", func(t *testing.T) { phaseGetTransactionReceipt(t, f) })
	t.Run("evm_get_block", func(t *testing.T) { phaseGetBlock(t, f) })
	t.Run("evm_get_logs", func(t *testing.T) { phaseGetLogs(t, f) })

	// Finally: prove the run above actually touched everything the server
	// advertises. This is what keeps the suite honest as tools are added.
	t.Run("coverage", func(t *testing.T) { phaseCoverage(t, f) })
}

// phaseCoverage asserts the run invoked every advertised tool. It is the
// mechanism behind this package's promise: a tool added to the server
// without a file here turns into a failing test, not a silent gap.
//
// It is a runtime check, not a registry of file names -- it counts what
// f.call actually sent. A per-tool file that exists but never reaches its
// tool fails just as loudly as a missing one.
func phaseCoverage(t *testing.T, f *flow) {
	var missed []string
	for _, name := range f.advertisedTools {
		if !f.calledTools[name] {
			missed = append(missed, name)
		}
	}
	if len(missed) > 0 {
		t.Errorf("these advertised tools were never called by this run: %v\n"+
			"add <tool_name>_test.go with a phase for each and wire it into TestE2E_AllTools, "+
			"or the suite no longer covers the server", missed)
	}

	// The inverse is a bug in this suite, not in the server: a phase
	// calling something the server does not advertise.
	for name := range f.calledTools {
		if !contains(f.advertisedTools, name) {
			t.Errorf("this run called %q, which the server does not advertise", name)
		}
	}

	t.Logf("covered %d/%d advertised tools", len(f.calledTools), len(f.advertisedTools))
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
