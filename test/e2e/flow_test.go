// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
// on-chain cost to one registry and one record per run.
//
// Phases that later phases depend on are guarded: if such a phase fails,
// the run stops rather than cascading misleading failures.
func TestE2E_AllTools(t *testing.T) {
	f := newFlow(t)

	// Discovery and identity. Everything downstream reads chain ID and
	// the anchor precompile address from what these phases record, so the
	// suite adapts to whichever deployment it is pointed at.
	mustRun(t, "tools_list", func(t *testing.T) { phaseToolsList(t, f) })
	mustRun(t, "nvnm_overview", func(t *testing.T) { phaseOverview(t, f) })
	mustRun(t, "evm_get_chain_id", func(t *testing.T) { phaseChainID(t, f) })

	// Onboarding: the journey nvnm_overview itself recommends.
	mustRun(t, "nvnm_setup_wizard", func(t *testing.T) { phaseSetupWizard(t, f) })
	t.Run("nvnm_setup_verify_hash", func(t *testing.T) { phaseVerifyHash(t, f) })
	t.Run("nvnm_setup_verify_signature", func(t *testing.T) { phaseVerifySignature(t, f) })

	mustRun(t, "wallet_status", func(t *testing.T) { phaseWalletStatus(t, f) })

	// Anchor reads that need no state of our own.
	t.Run("anchor_info", func(t *testing.T) { phaseAnchorInfo(t, f) })
	t.Run("anchor_get_registries_listing", func(t *testing.T) { phaseGetRegistriesListing(t, f) })

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

	// Write path: create a registry, then read it back by every route.
	mustRun(t, "anchor_prepare_add_registry", func(t *testing.T) { phaseAddRegistry(t, f) })
	mustRun(t, "anchor_get_registries_by_name", func(t *testing.T) { phaseResolveRegistryID(t, f) })
	t.Run("anchor_get_registry", func(t *testing.T) { phaseGetRegistry(t, f) })

	// Anchor a record, then read it back by every documented lookup mode.
	mustRun(t, "anchor_prepare_add_record", func(t *testing.T) { phaseAddRecord(t, f) })
	mustRun(t, "anchor_get_records", func(t *testing.T) { phaseGetRecords(t, f) })

	// Mutate that record's status and prove the change stuck on chain.
	t.Run("anchor_prepare_update_record_status", func(t *testing.T) { phaseUpdateRecordStatus(t, f) })

	// Registry access control.
	t.Run("anchor_prepare_grant_role", func(t *testing.T) { phaseGrantRole(t, f) })
	t.Run("anchor_prepare_revoke_role", func(t *testing.T) { phaseRevokeRole(t, f) })

	// EVM reads, pointed at the transaction and block this run produced,
	// so they assert against data with a known expected shape.
	t.Run("evm_get_transaction", func(t *testing.T) { phaseGetTransaction(t, f) })
	t.Run("evm_get_block", func(t *testing.T) { phaseGetBlock(t, f) })
	t.Run("evm_get_balance", func(t *testing.T) { phaseGetBalance(t, f) })
	t.Run("evm_get_code", func(t *testing.T) { phaseGetCode(t, f) })
	t.Run("evm_get_logs", func(t *testing.T) { phaseGetLogs(t, f) })
	t.Run("evm_call_contract", func(t *testing.T) { phaseCallContract(t, f) })

	// Finally: prove the run above actually touched everything the server
	// advertises. This is what keeps the suite honest as tools are added.
	t.Run("coverage", func(t *testing.T) { phaseCoverage(t, f) })
}

// mustRun runs a phase the rest of the flow depends on. A failure there
// aborts the run instead of cascading into unrelated failures downstream.
func mustRun(t *testing.T, name string, fn func(*testing.T)) {
	t.Helper()
	if !t.Run(name, fn) {
		t.Fatalf("phase %q failed; later phases depend on it, aborting the flow", name)
	}
}

// phaseToolsList records the server's advertised tool set, which the
// coverage phase later checks this run against.
func phaseToolsList(t *testing.T, f *flow) {
	res, err := f.session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	f.advertisedTools = names
	t.Logf("server advertises %d tools: %v", len(names), names)

	// The write tools are what make this suite an end-to-end test rather
	// than a read-only smoke test. Record their availability for the
	// parent to act on.
	f.writeToolsAvailable = true
	for _, required := range []string{
		"anchor_prepare_add_registry",
		"anchor_prepare_add_record",
		"evm_send_raw_transaction",
	} {
		if !contains(names, required) {
			t.Logf("server does not advertise %q; the write path is unavailable", required)
			f.writeToolsAvailable = false
		}
	}
}

// phaseCoverage asserts the run invoked every advertised tool. It is the
// mechanism behind this package's promise: a tool added to the server
// without a phase here turns into a failing test, not a silent gap.
func phaseCoverage(t *testing.T, f *flow) {
	var missed []string
	for _, name := range f.advertisedTools {
		if !f.calledTools[name] {
			missed = append(missed, name)
		}
	}
	if len(missed) > 0 {
		t.Errorf("these advertised tools were never called by this run: %v\n"+
			"add a phase for each in test/e2e, or the suite no longer covers the server", missed)
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
