// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// anchor_prepare_update_record_status -- changing the status of an
// already-anchored record.
//
// This is the deepest file in the suite, because the tool's one
// interesting parameter is the one that cannot be checked without state.
//
// `index` selects which version of a record to update, and the tool
// schema and docs/TOOL_REFERENCE.md both call it optional, defaulting to
// the latest version. The ABI has no way to express "absent" --
// abi/anchoring.json declares a plain uint64, and version indexes are
// 1-based, so the 0 an omitted index would encode as names no version at
// all. The server closes that gap itself: PrepareUpdateRecordStatus
// resolves nil and 0 alike to a concrete index with one `records` read
// before packing the call, so the transaction a caller signs always
// names the version it will move.
//
// That makes the write path agree with the read side, which is where the
// "default: latest" wording came from in the first place:
// anchor_get_records takes *uint64 and the precompile treats a zero
// index as "latest". Both tools now spell "latest" the same two ways.
//
// So the claims under test here are chain-level, not code-level: an
// omitted index must move the latest version, an explicit 0 must do
// exactly what omitting it does, and neither may touch any other
// version. The phases are ordered by how much they can see:
//
//	phaseUpdateRecordStatus           one version  -- the happy path and the rejections
//	phaseUpdateRecordStatusVersioned  three versions -- where index actually selects something
//
// A single-version record cannot distinguish "latest" from "version 1"
// from "whatever 0 means", which is why the versioned phase exists.

// versionCount is how many versions of one document the versioned phase
// anchors. Three is the smallest number that gives a latest version, a
// middle one, and an oldest one that is unambiguously historical.
const versionCount = 3

// phaseUpdateRecordStatus drives the tool against the single-version
// fixture record: the documented call form, the read-back, and the input
// rejections.
func phaseUpdateRecordStatus(t *testing.T, f *flow) {
	const wantStatus = "Revoked"

	t.Logf("fixture record: record_id=%d index=%d", f.recordID, f.recordIndex)

	// The documented form: no index at all, which means "latest".
	if outcome := tryUpdateStatus(t, f, f.recordID, nil, wantStatus); !outcome.applied {
		t.Fatalf("updating with index omitted %s -- but index is documented as optional, "+
			"defaulting to the latest version, in both the tool schema and "+
			"docs/TOOL_REFERENCE.md, and the server resolves an omitted index to the "+
			"record's current version before building the transaction", outcome.describe())
	}

	// The update must be visible on a subsequent read.
	var out recordsResponse
	f.callOK(t, "anchor_get_records", map[string]any{
		"registry_id": f.registryID,
		"limit":       10,
	}, &out)

	var found bool
	for _, rec := range out.Records {
		if rec.Checksum == f.checksum {
			found = true
			if rec.Status != wantStatus {
				t.Errorf("status = %q after a confirmed updateRecordStatus, want %q", rec.Status, wantStatus)
			}
			t.Logf("status confirmed on chain: record_id=%d status=%s", rec.RecordID, rec.Status)
			break
		}
	}
	if !found {
		t.Errorf("checksum %q disappeared from registry %d after the status update", f.checksum, f.registryID)
	}

	// Rejections. None of these reach the chain as a write: the tool
	// either validates client-side or the revert surfaces during gas
	// estimation, so this block costs nothing.
	index := f.recordIndex
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"no from", map[string]any{
			"registry_id": f.registryID, "record_id": f.recordID, "index": index, "status": "Active",
		}},
		{"registry_id 0", map[string]any{
			"from": f.address, "registry_id": 0, "record_id": f.recordID, "index": index, "status": "Active",
		}},
		{"record_id 0", map[string]any{
			"from": f.address, "registry_id": f.registryID, "record_id": 0, "index": index, "status": "Active",
		}},
		{"no status", map[string]any{
			"from": f.address, "registry_id": f.registryID, "record_id": f.recordID, "index": index,
		}},
		{"unknown record_id", map[string]any{
			"from": f.address, "registry_id": f.registryID, "record_id": uint64(1 << 40),
			"index": index, "status": "Active",
		}},
		// The same record, with the index omitted, is rejected a step
		// earlier: there is no latest version to resolve, and the server
		// will not invent one to hand to gas estimation.
		{"unknown record_id, index omitted", map[string]any{
			"from": f.address, "registry_id": f.registryID, "record_id": uint64(1 << 40),
			"status": "Active",
		}},
		{"index past the last version", map[string]any{
			"from": f.address, "registry_id": f.registryID, "record_id": f.recordID,
			"index": index + 100, "status": "Active",
		}},
	} {
		if got, errText := f.tryPrepare(t, "anchor_prepare_update_record_status", tc.args); got != nil {
			t.Errorf("%s: the tool returned a transaction instead of rejecting the call", tc.name)
		} else {
			t.Logf("%s: rejected -- %s", tc.name, errText)
		}
	}
}

// phaseUpdateRecordStatusVersioned is the case the single-version phase
// cannot be. It anchors several versions of one document, then updates
// the latest version and a historical one, and checks each update landed
// on the version it named and on no other.
//
// Three versions are three distinct rows, so "index omitted", "index =
// the latest version" and "index = an older version" now select
// different things -- and the first two must select the same thing.
// Every version is read back by
// (registry_id, record_id, index) after each write -- the only lookup
// mode that can see a superseded version, and so the only way to know
// which row moved.
func phaseUpdateRecordStatusVersioned(t *testing.T, f *flow) {
	versions := anchorVersions(t, f, versionCount)

	// Did the chain treat those as versions of one record, or as
	// unrelated records? Everything below depends on the answer, and no
	// amount of client-side reasoning settles it: the precompile assigns
	// both record_id and index.
	recordID := versions[0].RecordID
	for _, v := range versions[1:] {
		if v.RecordID != recordID {
			ids := make([]uint64, 0, len(versions))
			indexes := make([]uint64, 0, len(versions))
			for _, x := range versions {
				ids = append(ids, x.RecordID)
				indexes = append(indexes, x.Index)
			}
			t.Fatalf("%d addRecord calls with one URI and one checksum produced record_ids %v "+
				"(indexes %v), not one versioned record: anchor_prepare_add_record sends "+
				"record_id=0/index=0 in the tuple and exposes no way to add a version to an existing "+
				"record, so with re-anchoring ruled out too, the latest-vs-historical distinction that "+
				"the index parameter exists for would be unreachable through the MCP surface",
				len(versions), ids, indexes)
		}
	}

	latest, historical := &versions[0], &versions[0]
	for i := range versions {
		if versions[i].Index > latest.Index {
			latest = &versions[i]
		}
		if versions[i].Index < historical.Index {
			historical = &versions[i]
		}
	}
	if latest.Index == historical.Index {
		t.Fatalf("all %d versions of record %d report index=%d; there is no latest/historical "+
			"distinction to test", len(versions), recordID, latest.Index)
	}
	t.Logf("record %d has %d versions: latest index=%d, oldest index=%d",
		recordID, len(versions), latest.Index, historical.Index)

	// Baseline: what every version's status was before any update. Read
	// from chain rather than assumed, so the comparisons below are
	// against what the precompile actually stored.
	before := readVersions(t, f, recordID, versions)

	// The documented default. "index optional, default: latest" is a
	// promise about this exact call, and with several versions it is
	// finally falsifiable: only now can a landed update be attributed to
	// the latest version rather than merely to some version.
	var omitted updateOutcome
	t.Run("index_omitted", func(t *testing.T) {
		const want = "Superseded"
		omitted = tryUpdateStatus(t, f, recordID, nil, want)
		if !omitted.applied {
			t.Errorf("updating with index omitted %s -- the tool schema and "+
				"docs/TOOL_REFERENCE.md document index as optional, defaulting to the latest "+
				"version, and the server resolves it against the chain before signing",
				omitted.describe())
			assertUnchanged(t, f, recordID, versions, before)
			return
		}
		assertOnlyChanged(t, f, recordID, versions, before, latest.Index, want)
		before = readVersions(t, f, recordID, versions)
	})

	// An explicit index=0. Version indexes are 1-based, so 0 names no
	// version and both tools read it as "latest" -- the server resolves
	// it exactly as it resolves an omitted index. This case asserts the
	// two forms agree rather than asserting either one's outcome: a
	// caller who writes 0 and a caller who writes nothing must not end up
	// moving different rows.
	t.Run("index_zero", func(t *testing.T) {
		const want = "Superseded"
		zero := uint64(0)
		outcome := tryUpdateStatus(t, f, recordID, &zero, want)
		if outcome.applied != omitted.applied {
			t.Errorf("index=0 %s but omitting index %s; the two must be indistinguishable, "+
				"since the server resolves both to the record's latest version",
				outcome.describe(), omitted.describe())
		}
		if !outcome.applied {
			t.Logf("index=0 %s, exactly as the omitted form did", outcome.describe())
			assertUnchanged(t, f, recordID, versions, before)
			return
		}
		assertOnlyChanged(t, f, recordID, versions, before, latest.Index, want)
		before = readVersions(t, f, recordID, versions)
	})

	// The same update, naming the latest version explicitly. This is the
	// tool's whole job -- changing a record's current status -- and it
	// must work whether or not the caller leans on the default.
	t.Run("explicit_latest_index", func(t *testing.T) {
		const want = "Revoked"
		outcome := tryUpdateStatus(t, f, recordID, &latest.Index, want)
		if !outcome.applied {
			t.Errorf("updating the latest version (index=%d) explicitly %s; a caller must be able "+
				"to change the current status of a record", latest.Index, outcome.describe())
			assertUnchanged(t, f, recordID, versions, before)
			return
		}
		assertOnlyChanged(t, f, recordID, versions, before, latest.Index, want)
		before = readVersions(t, f, recordID, versions)
	})

	// A historical version. Whether the precompile allows rewriting the
	// status of a superseded version is its call, and this suite does not
	// presume the answer -- both outcomes are recorded. What is asserted
	// either way is that the write agreed with the read: if it landed,
	// the named version changed and nothing else did; if it was rejected,
	// nothing changed at all. A call that reports success while moving a
	// different row is the failure this case exists to catch.
	t.Run("explicit_historical_index", func(t *testing.T) {
		const want = "Superseded"
		outcome := tryUpdateStatus(t, f, recordID, &historical.Index, want)
		if !outcome.applied {
			t.Logf("the precompile refuses to update historical version index=%d of record %d: "+
				"%s. That is a legitimate immutability rule, but nothing in the tool schema or "+
				"docs/TOOL_REFERENCE.md says so -- index is documented as a plain version selector",
				historical.Index, recordID, outcome.describe())
			assertUnchanged(t, f, recordID, versions, before)
			return
		}
		t.Logf("the precompile accepts updating historical version index=%d of record %d",
			historical.Index, recordID)
		assertOnlyChanged(t, f, recordID, versions, before, historical.Index, want)
	})
}

// anchorVersions calls addRecord n times with the same URI and the same
// checksum, and returns each resulting row as the chain stored it, in
// creation order.
//
// Repeating the checksum is what makes these versions rather than
// neighbours: anchor_prepare_add_record sends record_id=0 and index=0 in
// the tuple and offers no "new version of record N" input, so the
// precompile alone decides, and it decides on content. Re-anchoring the
// same URI under a *different* checksum produces an unrelated record with
// its own record_id -- which is why this helper varies only the metadata
// between calls. Whether the chain still agrees is the first thing the
// caller checks.
func anchorVersions(t *testing.T, f *flow, n int) []record {
	t.Helper()

	suffix := uniqueSuffix()
	uri := "https://example.invalid/nvnm-e2e/versioned/" + suffix
	sum := sha256.Sum256([]byte("nvnm mcp e2e versioned document " + suffix))
	checksum := hex.EncodeToString(sum[:])

	versions := make([]record, 0, n)
	for i := 1; i <= n; i++ {
		utx := f.prepare(t, "anchor_prepare_add_record", map[string]any{
			"from":          f.address,
			"registry_id":   f.registryID,
			"uri":           uri,
			"checksum":      checksum,
			"checksum_algo": "sha256",
			"metadata":      fmt.Sprintf(`{"suite":"test/e2e","version":%d}`, i),
		})
		f.signBroadcastConfirm(t, utx)

		// A checksum lookup returns the latest version holding it, which
		// is the one this iteration just wrote.
		var out recordsResponse
		f.callOK(t, "anchor_get_records", map[string]any{
			"registry_id": f.registryID,
			"checksum":    checksum,
		}, &out)

		var found *record
		for j := range out.Records {
			if out.Records[j].Checksum == checksum {
				found = &out.Records[j]
				break
			}
		}
		if found == nil {
			t.Fatalf("v%d: checksum %s not found in registry %d after a confirmed addRecord",
				i, checksum, f.registryID)
		}
		t.Logf("v%d anchored at %s: record_id=%d index=%d is_latest=%v status=%s",
			i, uri, found.RecordID, found.Index, found.IsLatest, found.Status)
		versions = append(versions, *found)
	}
	return versions
}

// readVersions fetches every version of a record by its version index --
// anchor_get_records lookup mode 1, the only mode that can see a
// superseded version at all, and so the only way to check that an update
// hit the version it named.
func readVersions(t *testing.T, f *flow, recordID uint64, versions []record) map[uint64]record {
	t.Helper()

	out := make(map[uint64]record, len(versions))
	for _, v := range versions {
		var resp recordsResponse
		f.callOK(t, "anchor_get_records", map[string]any{
			"registry_id": f.registryID,
			"record_id":   recordID,
			"index":       v.Index,
		}, &resp)

		var found *record
		for i := range resp.Records {
			if resp.Records[i].Index == v.Index {
				found = &resp.Records[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("record_id=%d index=%d returned no such version (%d records returned); "+
				"anchor_get_records documents this as lookup mode 1, by specific version",
				recordID, v.Index, len(resp.Records))
		}
		out[v.Index] = *found
	}
	return out
}

// updateOutcome is what became of one status update: whether it reached
// the chain, and if not, how it failed. Rejection at prepare time and
// reversion at broadcast time are both "not applied" -- the anchor_prepare
// tools estimate gas, which runs the precompile, so an input the
// precompile refuses usually surfaces as a tool error before anything is
// signed.
type updateOutcome struct {
	applied      bool
	toolError    string
	receiptState string
}

func (o updateOutcome) describe() string {
	switch {
	case o.applied:
		return "was applied on chain"
	case o.toolError != "":
		return "was rejected by the tool: " + o.toolError
	default:
		return "reverted on chain with receipt status " + o.receiptState
	}
}

// tryUpdateStatus runs one anchor_prepare_update_record_status through
// the full prepare-sign-broadcast path without asserting it succeeds. A
// nil index omits the parameter entirely, which is the documented
// "latest" form.
func tryUpdateStatus(
	t *testing.T, f *flow, recordID uint64, index *uint64, status string,
) updateOutcome {
	t.Helper()

	args := map[string]any{
		"from":        f.address,
		"registry_id": f.registryID,
		"record_id":   recordID,
		"status":      status,
	}
	if index != nil {
		args["index"] = *index
	}

	utx, toolErr := f.tryPrepare(t, "anchor_prepare_update_record_status", args)
	if utx == nil {
		return updateOutcome{toolError: toolErr}
	}

	r := f.signBroadcastAllowRevert(t, utx)
	if r.Status != "success" {
		return updateOutcome{receiptState: r.Status}
	}
	return updateOutcome{applied: true}
}

// assertOnlyChanged checks that a landed update moved the version it
// named to wantStatus and left every other version alone. The second half
// is the point: an update that reports success while changing a different
// version is precisely the failure a mis-resolved index default would
// produce, and the only way to catch it is to read every version back.
func assertOnlyChanged(
	t *testing.T,
	f *flow,
	recordID uint64,
	versions []record,
	before map[uint64]record,
	changedIndex uint64,
	wantStatus string,
) {
	t.Helper()

	after := readVersions(t, f, recordID, versions)
	for idx, rec := range after {
		switch {
		case idx == changedIndex && rec.Status != wantStatus:
			t.Errorf("index=%d: status = %q after an update that targeted it, want %q",
				idx, rec.Status, wantStatus)
		case idx != changedIndex && rec.Status != before[idx].Status:
			t.Errorf("index=%d: status changed from %q to %q, but the update targeted index=%d; "+
				"the update landed on a version the caller did not name",
				idx, before[idx].Status, rec.Status, changedIndex)
		}
	}
	t.Logf("after the update: %s", statusLine(versions, after))
}

// assertUnchanged checks that a rejected update left the record exactly
// as it was. A rejection that still moved something on chain is worse
// than either outcome on its own.
func assertUnchanged(
	t *testing.T, f *flow, recordID uint64, versions []record, before map[uint64]record,
) {
	t.Helper()

	after := readVersions(t, f, recordID, versions)
	for idx, rec := range after {
		if rec.Status != before[idx].Status {
			t.Errorf("index=%d: status changed from %q to %q even though the update never landed",
				idx, before[idx].Status, rec.Status)
		}
	}
	t.Logf("after the rejected update: %s", statusLine(versions, after))
}

// statusLine renders every version's status in index order for the log.
func statusLine(versions []record, byIndex map[uint64]record) string {
	var b strings.Builder
	for i, v := range versions {
		if i > 0 {
			b.WriteString(" ")
		}
		rec := byIndex[v.Index]
		fmt.Fprintf(&b, "index=%d:%s(latest=%v)", v.Index, rec.Status, rec.IsLatest)
	}
	return b.String()
}
