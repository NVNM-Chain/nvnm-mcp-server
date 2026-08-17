// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// Anchor tools: anchor_info, anchor_get_registry, anchor_get_registries,
// anchor_get_records, and the five anchor_prepare_* write tools.
//
// The write phases follow the prepare-sign-submit contract end to end:
// the server returns unsigned bytes, this process signs them locally, and
// evm_send_raw_transaction broadcasts. Each write is then read back
// through the corresponding query tool, so a write that silently did
// nothing on chain fails here.

// granteeAddress receives (and then loses) the editor role during the
// grant/revoke phases. A burn address keeps the grant inert: the run
// hands real authority to nothing that can use it.
const granteeAddress = "0x0000000000000000000000000000000000000001"

type registry struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Creator     string `json:"creator"`
	CreatedAt   string `json:"created_at"`
	Metadata    string `json:"metadata"`
}

type record struct {
	RegistryID   uint64 `json:"registry_id"`
	RecordID     uint64 `json:"record_id"`
	Index        uint64 `json:"index"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	URI          string `json:"uri"`
	Status       string `json:"status"`
	IsLatest     bool   `json:"is_latest"`
	Timestamp    string `json:"timestamp"`
	Metadata     string `json:"metadata"`
}

// pageResponse mirrors the Cosmos-style pagination envelope. Note that
// this chain's precompile always reports total=0; the server substitutes
// its own scan count on the listing paths, which is why this is logged
// rather than asserted on.
type pageResponse struct {
	Total uint64 `json:"total"`
}

type registriesResponse struct {
	Registries        []registry    `json:"registries"`
	Pagination        *pageResponse `json:"pagination"`
	ContentTrust      string        `json:"content_trust"`
	TotalIsLowerBound bool          `json:"total_is_lower_bound"`
}

type registryResponse struct {
	registry
	ContentTrust string `json:"content_trust"`
}

type recordsResponse struct {
	Records      []record      `json:"records"`
	Pagination   *pageResponse `json:"pagination"`
	ContentTrust string        `json:"content_trust"`
}

// phaseAnchorInfo checks the precompile the server is wired to matches
// the one it advertised in the overview.
func phaseAnchorInfo(t *testing.T, f *flow) {
	var out struct {
		Address     string `json:"address"`
		ChainID     int64  `json:"chain_id"`
		ABILoaded   bool   `json:"abi_loaded"`
		MethodCount int    `json:"method_count"`
	}
	f.callOK(t, "anchor_info", map[string]any{}, &out)

	if !out.ABILoaded {
		t.Error("abi_loaded is false; the anchor write tools cannot encode calldata without it")
	}
	if out.MethodCount == 0 {
		t.Error("method_count is 0")
	}
	if !strings.EqualFold(out.Address, f.anchorAddress) {
		t.Errorf("anchor_info address = %s, but nvnm_overview reported %s", out.Address, f.anchorAddress)
	}
	if out.ChainID != f.chainID {
		t.Errorf("anchor_info chain_id = %d, want %d", out.ChainID, f.chainID)
	}
	t.Logf("precompile %s: abi_loaded=%v methods=%d", out.Address, out.ABILoaded, out.MethodCount)
}

// phaseGetRegistriesListing exercises the unfiltered listing mode, whose
// paging is applied client-side after a full scan of the registry table.
func phaseGetRegistriesListing(t *testing.T, f *flow) {
	var out registriesResponse
	f.callOK(t, "anchor_get_registries", map[string]any{"limit": 5}, &out)

	if len(out.Registries) > 5 {
		t.Errorf("limit=5 returned %d registries", len(out.Registries))
	}
	if out.ContentTrust == "" {
		t.Error("content_trust is empty; on-chain names and metadata are untrusted input and must be labelled as such")
	}
	var total uint64
	if out.Pagination != nil {
		total = out.Pagination.Total
	}
	t.Logf("listing returned %d registries (scanned total=%d lower_bound=%v)",
		len(out.Registries), total, out.TotalIsLowerBound)
}

// phaseAddRegistry creates the registry this run writes into.
func phaseAddRegistry(t *testing.T, f *flow) {
	f.registryName = "mcp-e2e-" + uniqueSuffix()

	utx := f.prepare(t, "anchor_prepare_add_registry", map[string]any{
		"from":        f.address,
		"name":        f.registryName,
		"description": "NVNM MCP end-to-end test run",
		"metadata":    `{"suite":"test/e2e"}`,
	})
	f.signBroadcastConfirm(t, utx)
	t.Logf("created registry %q", f.registryName)
}

// phaseResolveRegistryID finds the new registry by name. The precompile
// has no on-chain by-name index, so this is a client-side scan inside the
// server -- worth exercising against a real table rather than a mock.
func phaseResolveRegistryID(t *testing.T, f *flow) {
	var out registriesResponse
	f.callOK(t, "anchor_get_registries", map[string]any{
		"name":  f.registryName,
		"match": "exact",
		"limit": 10,
	}, &out)

	for _, reg := range out.Registries {
		if reg.Name == f.registryName {
			f.registryID = reg.ID
			t.Logf("resolved registry %q -> id=%d creator=%s", reg.Name, reg.ID, reg.Creator)
			return
		}
	}
	t.Fatalf("registry %q not found by name after a confirmed addRegistry (scanned %d, truncated=%v)",
		f.registryName, len(out.Registries), out.TotalIsLowerBound)
}

// phaseGetRegistry reads the registry back by ID and checks the fields
// survived the round trip through the precompile.
func phaseGetRegistry(t *testing.T, f *flow) {
	var out registryResponse
	f.callOK(t, "anchor_get_registry", map[string]any{"id": f.registryID}, &out)

	if out.ID != f.registryID {
		t.Errorf("id = %d, want %d", out.ID, f.registryID)
	}
	if out.Name != f.registryName {
		t.Errorf("name = %q, want %q", out.Name, f.registryName)
	}
	if !strings.EqualFold(out.Creator, f.address) {
		t.Errorf("creator = %s, want the signing wallet %s", out.Creator, f.address)
	}
	if out.ContentTrust == "" {
		t.Error("content_trust is empty")
	}
}

// phaseAddRecord anchors a checksum + URI into the new registry.
func phaseAddRecord(t *testing.T, f *flow) {
	// Anchor a genuine SHA-256 digest of a synthetic document rather than
	// a made-up string: 64 hex chars is exactly what the precompile
	// expects, and it keeps the fixture honest about what a checksum is.
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
	t.Logf("anchored checksum=%s in block %d", f.checksum, r.BlockNumber)
}

// phaseGetRecords reads the new record back through every lookup mode
// anchor_get_records documents that this run has the inputs for.
func phaseGetRecords(t *testing.T, f *flow) {
	// Mode 4: all latest records in a registry.
	var byRegistry recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"limit":       10,
	}, &byRegistry)

	var found *record
	for i := range byRegistry.Records {
		if byRegistry.Records[i].Checksum == f.checksum {
			found = &byRegistry.Records[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("checksum %q not found in registry %d after a confirmed addRecord (%d records returned)",
			f.checksum, f.registryID, len(byRegistry.Records))
	}
	if found.URI != f.uri {
		t.Errorf("uri = %q, want %q", found.URI, f.uri)
	}
	if found.ChecksumAlgo != "sha256" {
		t.Errorf("checksum_algo = %q, want %q", found.ChecksumAlgo, "sha256")
	}

	f.recordID = found.RecordID
	f.recordIndex = found.Index
	t.Logf("record: record_id=%d index=%d status=%s is_latest=%v",
		found.RecordID, found.Index, found.Status, found.IsLatest)

	// Mode 3: content-hash lookup within the registry.
	var byChecksum recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"checksum":    f.checksum,
	}, &byChecksum)
	if len(byChecksum.Records) == 0 {
		t.Error("checksum lookup returned nothing for a checksum that exists in the registry")
	}

	// Mode 2: latest version of a specific record.
	var byRecordID recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"record_id":   f.recordID,
	}, &byRecordID)
	if len(byRecordID.Records) == 0 {
		t.Errorf("record_id lookup returned nothing for record_id=%d", f.recordID)
	}
}

// phaseUpdateRecordStatus flips the record to Revoked and confirms the
// new status is what a subsequent read returns.
//
// index is deliberately omitted: the tool treats a missing index as "the
// latest version", which is what a caller updating a record's current
// status means. Pinning an explicit index targets one historical version
// instead and is rejected on chain.
func phaseUpdateRecordStatus(t *testing.T, f *flow) {
	const wantStatus = "Revoked"

	utx := f.prepare(t, "anchor_prepare_update_record_status", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"record_id":   f.recordID,
		"status":      wantStatus,
	})
	f.signBroadcastConfirm(t, utx)

	var out recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"limit":       10,
	}, &out)

	for _, rec := range out.Records {
		if rec.Checksum == f.checksum {
			if rec.Status != wantStatus {
				t.Errorf("status = %q after a confirmed updateRecordStatus, want %q", rec.Status, wantStatus)
			}
			t.Logf("status confirmed on chain: record_id=%d status=%s", rec.RecordID, rec.Status)
			return
		}
	}
	t.Errorf("checksum %q disappeared from registry %d after the status update", f.checksum, f.registryID)
}

// phaseGrantRole grants the editor role on the new registry. The signing
// wallet created the registry, so it is the admin and the grant is
// authorized on chain.
func phaseGrantRole(t *testing.T, f *flow) {
	utx := f.prepare(t, "anchor_prepare_grant_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"account":     granteeAddress,
		"role":        "editor",
	})
	r := f.signBroadcastConfirm(t, utx)
	t.Logf("granted editor on registry %d to %s (gas %d)", f.registryID, granteeAddress, r.GasUsed)
}

// phaseRevokeRole takes the role granted above back, leaving the chain in
// the state the run started from as far as access control is concerned.
func phaseRevokeRole(t *testing.T, f *flow) {
	utx := f.prepare(t, "anchor_prepare_revoke_role", map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"account":     granteeAddress,
		"role":        "editor",
	})
	r := f.signBroadcastConfirm(t, utx)
	t.Logf("revoked editor on registry %d from %s (gas %d)", f.registryID, granteeAddress, r.GasUsed)
}
