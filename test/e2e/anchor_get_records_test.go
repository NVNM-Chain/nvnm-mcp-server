// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// anchor_get_records -- the flexible record query. It documents five
// lookup modes, and this phase drives every one the fixture has inputs
// for, then checks they agree with each other.
//
// Mode 1 (by version index) is the one that matters most and is easiest
// to overlook: it is the only mode that can see a superseded version.
// Every other mode returns the latest, so without mode 1 there is no way
// to observe record history at all.

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
		t.Fatalf("checksum %q not found in registry %d (%d records returned)",
			f.checksum, f.registryID, len(byRegistry.Records))
	}
	if found.URI != f.uri {
		t.Errorf("uri = %q, want %q", found.URI, f.uri)
	}
	if found.ChecksumAlgo != "sha256" {
		t.Errorf("checksum_algo = %q, want %q", found.ChecksumAlgo, "sha256")
	}
	if found.RegistryID != f.registryID {
		t.Errorf("registry_id = %d, want %d", found.RegistryID, f.registryID)
	}
	if !found.IsLatest {
		t.Errorf("is_latest = false for record_id=%d index=%d, but a registry listing returns "+
			"latest versions only", found.RecordID, found.Index)
	}
	if found.Timestamp == "" {
		t.Error("timestamp is empty")
	}
	if byRegistry.ContentTrust == "" {
		t.Error("content_trust is empty; record URIs and metadata are untrusted on-chain input")
	}
	t.Logf("record: record_id=%d index=%d status=%s is_latest=%v",
		found.RecordID, found.Index, found.Status, found.IsLatest)

	// Mode 3: content-hash lookup within the registry.
	var byChecksum recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"checksum":    f.checksum,
	}, &byChecksum)
	if len(byChecksum.Records) == 0 {
		t.Fatal("checksum lookup returned nothing for a checksum that exists in the registry")
	}
	if byChecksum.Records[0].RecordID != f.recordID {
		t.Errorf("checksum lookup returned record_id=%d, want %d",
			byChecksum.Records[0].RecordID, f.recordID)
	}

	// Mode 2: latest version of a specific record.
	var byRecordID recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"record_id":   f.recordID,
	}, &byRecordID)
	if len(byRecordID.Records) == 0 {
		t.Fatalf("record_id lookup returned nothing for record_id=%d", f.recordID)
	}
	if !byRecordID.Records[0].IsLatest {
		t.Error("a record_id lookup with no index must return the latest version")
	}

	// Mode 1: one specific version. Version indexes are 1-based, and the
	// fixture record has exactly one version, so its index is the only
	// one that resolves.
	var byIndex recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"record_id":   f.recordID,
		"index":       f.recordIndex,
	}, &byIndex)
	if len(byIndex.Records) == 0 {
		t.Fatalf("by-version lookup returned nothing for record_id=%d index=%d",
			f.recordID, f.recordIndex)
	}
	if byIndex.Records[0].Index != f.recordIndex {
		t.Errorf("by-version lookup for index=%d returned index=%d",
			f.recordIndex, byIndex.Records[0].Index)
	}

	// index=0 is how the read path spells "latest": GetRecordsRequest
	// takes *uint64 and treats absent as unset, and the precompile reads
	// a zero index as the latest version. Worth pinning, because the
	// write path does NOT accept 0 -- see anchor_prepare_update_record_status_test.go.
	var byZeroIndex recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"record_id":   f.recordID,
		"index":       0,
	}, &byZeroIndex)
	if len(byZeroIndex.Records) == 0 {
		t.Errorf("index=0 returned nothing for record_id=%d; on the read path a zero index "+
			"means the latest version, not version zero", f.recordID)
	} else if !byZeroIndex.Records[0].IsLatest {
		t.Errorf("index=0 returned index=%d with is_latest=false; a zero index means latest",
			byZeroIndex.Records[0].Index)
	}

	// Mode 5: checksum across all registries. Our checksum is a digest of
	// a per-run unique document, so the fixture registry must be among
	// the matches.
	var acrossRegistries recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{"checksum": f.checksum}, &acrossRegistries)
	if len(acrossRegistries.Records) == 0 {
		t.Error("cross-registry checksum lookup returned nothing for a checksum that exists")
	}
	for _, rec := range acrossRegistries.Records {
		if rec.Checksum != f.checksum {
			t.Errorf("cross-registry lookup for %s returned a record with checksum %s",
				f.checksum, rec.Checksum)
		}
	}
}
