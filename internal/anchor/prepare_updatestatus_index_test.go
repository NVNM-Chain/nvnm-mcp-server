// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// `index` selects which version of a record to update. It is documented as
// optional, "default: latest", but the ABI declares a plain uint64 -- there is
// no "absent" to encode -- and version indexes are 1-based, so the 0 an
// omitted index would otherwise encode as names no version at all. The client
// resolves nil and 0 alike to a concrete index via the `records` view before
// packing the call. These tests pin that resolution by decoding the calldata
// the client actually built, not by trusting the request it was handed.

// updateStatusProbe wires a client whose `records` view returns fixed rows and
// whose gas estimate captures the updateRecordStatus calldata, so a test can
// assert both which lookup was made and which index was encoded.
type updateStatusProbe struct {
	client       Client
	recordsCalls int
	recordsInput []byte
	txCalldata   []byte
}

func newUpdateStatusProbe(t *testing.T, rows []abiRecordRow, recordsErr error) *updateStatusProbe {
	t.Helper()
	p := &updateStatusProbe{}
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			p.recordsCalls++
			p.recordsInput = msg.Input
			if recordsErr != nil {
				return nil, recordsErr
			}
			return encodeRecordsOutput(t, rows, abiPaginationOutput{Total: uint64(len(rows))}), nil
		},
		estimateGasFn: func(_ context.Context, msg defitypes.Call) (uint64, error) {
			p.txCalldata = msg.Input
			return 100000, nil
		},
	}
	p.client = NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
	return p
}

// encodedIndex decodes the index argument out of the updateRecordStatus
// calldata the client built.
func (p *updateStatusProbe) encodedIndex(t *testing.T) uint64 {
	t.Helper()
	var registryID, recordID, index uint64
	var status string
	m := parsedTestABI(t).Methods["updateRecordStatus"]
	if err := m.DecodeArgs(p.txCalldata, &registryID, &recordID, &index, &status); err != nil {
		t.Fatalf("decode updateRecordStatus calldata: %v", err)
	}
	return index
}

// recordsLookup decodes the (registryId, checksum, recordId, index) filter the
// client used for its latest-version lookup.
func (p *updateStatusProbe) recordsLookup(t *testing.T) (registryID uint64, checksum string, recordID, index uint64) {
	t.Helper()
	var page abiPaginationInput
	m := parsedTestABI(t).Methods["records"]
	if err := m.DecodeArgs(p.recordsInput, &registryID, &checksum, &recordID, &index, &page); err != nil {
		t.Fatalf("decode records calldata: %v", err)
	}
	return registryID, checksum, recordID, index
}

func versionRow(recordID, index uint64, isLatest bool) abiRecordRow {
	return abiRecordRow{
		URI:          "https://example.invalid/doc",
		Checksum:     "abc123",
		ChecksumAlgo: "sha256",
		Metadata:     "{\"v\":1}",
		Timestamp:    "2026-08-17T00:00:00Z",
		Status:       "Active",
		RecordID:     recordID,
		Index:        index,
		IsLatest:     isLatest,
		RegistryID:   7,
	}
}

func validUpdateRequest(index *uint64) PrepareUpdateRecordStatusRequest {
	return PrepareUpdateRecordStatusRequest{
		From:       "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID: 7,
		RecordID:   42,
		Index:      index,
		Status:     "Revoked",
	}
}

func TestPrepareUpdateRecordStatus_ExplicitIndexEncodedVerbatim(t *testing.T) {
	p := newUpdateStatusProbe(t, nil, nil)

	index := uint64(3)
	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(&index)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 3 {
		t.Errorf("encoded index = %d, want 3", got)
	}
	// An explicit index needs no lookup: spending an eth_call on a question
	// the caller already answered would be waste on every write.
	if p.recordsCalls != 0 {
		t.Errorf("records was called %d times for an explicit index; want 0", p.recordsCalls)
	}
}

func TestPrepareUpdateRecordStatus_OmittedIndexResolvesLatest(t *testing.T) {
	rows := []abiRecordRow{
		versionRow(42, 1, false),
		versionRow(42, 2, false),
		versionRow(42, 3, true),
	}
	p := newUpdateStatusProbe(t, rows, nil)

	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 3 {
		t.Errorf("encoded index = %d, want 3 (the latest version)", got)
	}
	if p.recordsCalls != 1 {
		t.Errorf("records was called %d times; want exactly 1", p.recordsCalls)
	}
	registryID, checksum, recordID, index := p.recordsLookup(t)
	if registryID != 7 || checksum != "" || recordID != 42 || index != 0 {
		t.Errorf("lookup = (%d, %q, %d, %d), want (7, \"\", 42, 0)", registryID, checksum, recordID, index)
	}
}

// A zero index is how the read path spells "latest" (see
// anchor_get_records). The write path must not disagree with it: both forms
// have to produce the same transaction, byte for byte.
func TestPrepareUpdateRecordStatus_ZeroIndexMatchesOmitted(t *testing.T) {
	rows := []abiRecordRow{versionRow(42, 1, false), versionRow(42, 2, true)}

	omitted := newUpdateStatusProbe(t, rows, nil)
	if _, err := omitted.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("omitted index: %v", err)
	}

	zero := uint64(0)
	explicit := newUpdateStatusProbe(t, rows, nil)
	if _, err := explicit.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(&zero)); err != nil {
		t.Fatalf("index=0: %v", err)
	}

	if !bytes.Equal(omitted.txCalldata, explicit.txCalldata) {
		t.Errorf("index=0 built different calldata than omitting index:\n omitted:  %x\n index=0: %x",
			omitted.txCalldata, explicit.txCalldata)
	}
	if got := explicit.encodedIndex(t); got != 2 {
		t.Errorf("encoded index = %d, want 2 (the latest version)", got)
	}
}

// is_latest is the chain's own answer, so it wins over row order and over a
// higher index appearing earlier in the page.
func TestPrepareUpdateRecordStatus_PrefersIsLatestRow(t *testing.T) {
	rows := []abiRecordRow{versionRow(42, 5, false), versionRow(42, 2, true)}
	p := newUpdateStatusProbe(t, rows, nil)

	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 2 {
		t.Errorf("encoded index = %d, want 2 (the row flagged is_latest)", got)
	}
}

// If the chain ever stops flagging is_latest on a zero-index lookup, the
// highest version is still the latest one -- but rows for another record are
// never a candidate.
func TestPrepareUpdateRecordStatus_FallsBackToHighestIndex(t *testing.T) {
	rows := []abiRecordRow{
		versionRow(42, 1, false),
		versionRow(99, 7, false),
		versionRow(42, 4, false),
	}
	p := newUpdateStatusProbe(t, rows, nil)

	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 4 {
		t.Errorf("encoded index = %d, want 4 (highest index for record 42)", got)
	}
}

func TestPrepareUpdateRecordStatus_NoVersionsIsRecordNotFound(t *testing.T) {
	tests := []struct {
		name string
		rows []abiRecordRow
	}{
		{"empty result", nil},
		{"only other records", []abiRecordRow{versionRow(99, 1, true)}},
		{"zero index row", []abiRecordRow{versionRow(42, 0, true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newUpdateStatusProbe(t, tc.rows, nil)

			tx, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil))
			if err == nil {
				t.Fatalf("expected an error; got tx %+v", tx)
			}
			if !errors.Is(err, apperrors.ErrRecordNotFound) {
				t.Errorf("error should be ErrRecordNotFound; got %v", err)
			}
			if !apperrors.IsNotFound(err) {
				t.Errorf("error must classify as not-found so SafeForClient surfaces it; got %v", err)
			}
			if p.txCalldata != nil {
				t.Error("no transaction should be built when the record has no version to update")
			}
		})
	}
}

func TestPrepareUpdateRecordStatus_LookupFailurePropagates(t *testing.T) {
	p := newUpdateStatusProbe(t, nil, errors.New("rpc down"))

	tx, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil))
	if err == nil {
		t.Fatalf("expected an error; got tx %+v", tx)
	}
	if !strings.Contains(err.Error(), "resolve latest version index") {
		t.Errorf("error should name the step that failed; got %v", err)
	}
	if p.txCalldata != nil {
		t.Error("no transaction should be built when the latest-version lookup fails")
	}
}

// The precompile reverts on a key it does not hold rather than returning zero
// rows, so an unknown record_id arrives as a call failure, not an empty
// result. It must still reach the caller as "record not found" -- collapsed to
// a generic upstream failure, it tells them nothing they can act on.
func TestPrepareUpdateRecordStatus_UnknownRecordRevertIsNotFound(t *testing.T) {
	revert := errors.New(
		"RPC error: -32000 rpc error: code = Internal desc = collections: " +
			"not found: key '(\"7\", \"42\")' of type uint64",
	)
	p := newUpdateStatusProbe(t, nil, revert)

	tx, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil))
	if err == nil {
		t.Fatalf("expected an error; got tx %+v", tx)
	}
	if !errors.Is(err, apperrors.ErrRecordNotFound) {
		t.Errorf("error should be ErrRecordNotFound; got %v", err)
	}
	if !apperrors.IsNotFound(err) {
		t.Errorf("error must classify as not-found so SafeForClient surfaces it; got %v", err)
	}
	// The chain's raw revert text names internal storage keys and types;
	// SafeForClient passes not-found errors through verbatim, so the message
	// the client sees must not carry it.
	if strings.Contains(apperrors.SafeForClient(err).Error(), "collections") {
		t.Errorf("client-facing message leaks the raw revert: %v", apperrors.SafeForClient(err))
	}
}
