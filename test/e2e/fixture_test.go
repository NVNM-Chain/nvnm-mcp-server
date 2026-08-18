// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The shared fixture: the chain state every per-tool test asserts
// against, and the server facts they need to assert anything at all.
//
// These run in TestE2E_AllTools' own body rather than as subtests, which
// is what makes `go test -run 'TestE2E_AllTools/anchor_get_records'`
// work: -run filters subtests, so a prerequisite that lives in a subtest
// disappears under a filter and takes every later phase down with it.
// Hoisted here, the fixture is built whatever the filter selects.
//
// The division of labour is deliberate: setup functions *extract* (and
// fail loudly if they cannot), while per-tool phases *assert*. So
// setupDiscovery reads chain_id from nvnm_overview without judging the
// rest of that tool's response -- phaseOverview does that -- and
// setupRecord anchors a record without asserting anchor_prepare_add_record's
// output contract, which phasePrepareAddRecord does. A tool's assertions
// live in that tool's file, and only there.
//
// The cost is one registry and one record per run, whatever -run
// selects. That is unavoidable: you cannot read a record you have not
// written, and the tools are causally linked all the way down.

// setupDiscovery records what the server says about itself: its tool
// list, the chain it is configured for, and whether the signing wallet
// can pay for the write half of the run.
func setupDiscovery(t *testing.T, f *flow) {
	t.Helper()

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

	// Chain identity comes from the server, not a constant, so the suite
	// follows whichever deployment it is pointed at.
	var overview overviewResponse
	f.callOK(t, "nvnm_overview", map[string]any{}, &overview)
	if overview.ChainID == 0 {
		t.Fatal("nvnm_overview reports chain_id 0; nothing downstream can validate a prepared transaction")
	}
	if overview.AnchorPrecompile == "" {
		t.Fatal("nvnm_overview reports no anchor_precompile; nothing downstream can validate a destination")
	}
	f.chainID = overview.ChainID
	f.anchorAddress = overview.AnchorPrecompile
	t.Logf("chain=%s env=%s chain_id=%d precompile=%s",
		overview.ChainName, overview.ChainEnvironment, f.chainID, f.anchorAddress)

	var wallet walletStatusResponse
	f.callOK(t, "wallet_status", map[string]any{"address": f.address}, &wallet)
	f.walletFunded = wallet.Status != "unfunded"
	t.Logf("wallet %s: status=%s nonce=%d balance=%s",
		f.address, wallet.Status, wallet.Nonce, wallet.BalanceHuman)
}

// setupRegistry creates the registry this run writes into and resolves
// its chain-assigned ID. The precompile has no on-chain by-name index, so
// resolving means a server-side scan of the registry table -- phaseGetRegistries
// is where that scan's contract is asserted; here it is just how the ID
// is obtained.
func setupRegistry(t *testing.T, f *flow) {
	t.Helper()

	f.registryName = "mcp-e2e-" + uniqueSuffix()

	utx := f.prepare(t, "anchor_prepare_add_registry", map[string]any{
		"from":        f.address,
		"name":        f.registryName,
		"description": "NVNM MCP end-to-end test run",
		"metadata":    `{"suite":"test/e2e"}`,
	})
	f.signBroadcastConfirm(t, utx)

	var out registriesResponse
	f.callOK(t, "anchor_get_registries", map[string]any{
		"name":  f.registryName,
		"match": "exact",
		"limit": 10,
	}, &out)

	for _, reg := range out.Registries {
		if reg.Name == f.registryName {
			f.registryID = reg.ID
			t.Logf("fixture registry %q -> id=%d", reg.Name, reg.ID)
			return
		}
	}
	t.Fatalf("registry %q not found by name after a confirmed addRegistry (scanned %d, truncated=%v)",
		f.registryName, len(out.Registries), out.TotalIsLowerBound)
}

// setupRecord anchors the record the read phases query and the EVM phases
// point at. It anchors a genuine SHA-256 digest of a synthetic document
// rather than a made-up string: 64 hex chars is exactly what the
// precompile expects, and it keeps the fixture honest about what a
// checksum is.
func setupRecord(t *testing.T, f *flow) {
	t.Helper()

	suffix := uniqueSuffix()
	sum := sha256.Sum256([]byte("nvnm mcp e2e test document " + suffix))
	f.checksum = hex.EncodeToString(sum[:])
	f.uri = "https://example.invalid/nvnm-e2e/" + suffix

	utx := f.prepare(t, "anchor_prepare_add_record", map[string]any{
		"from":          f.address,
		"registry_id":   f.registryID,
		"uri":           f.uri,
		"checksum":      f.checksum,
		"checksum_algo": "sha256",
		"metadata":      `{"suite":"test/e2e"}`,
	})
	r := f.signBroadcastConfirm(t, utx)

	// Remember where this landed: the EVM read phases query this exact
	// transaction and block, so they assert against known data.
	f.recordTxHash = r.TxHash
	f.recordBlockNum = r.BlockNumber
	f.recordBlockHash = r.BlockHash

	// The chain assigns record_id and index; later phases need both.
	var out recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"checksum":    f.checksum,
	}, &out)
	for i := range out.Records {
		if out.Records[i].Checksum == f.checksum {
			f.recordID = out.Records[i].RecordID
			f.recordIndex = out.Records[i].Index
			t.Logf("fixture record: record_id=%d index=%d status=%s in block %d",
				f.recordID, f.recordIndex, out.Records[i].Status, r.BlockNumber)
			return
		}
	}
	t.Fatalf("checksum %s not found in registry %d after a confirmed addRecord",
		f.checksum, f.registryID)
}
