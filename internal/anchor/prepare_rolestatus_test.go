// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// PrepareUpdateRecordStatus and PrepareRevokeRole arrived with the rc17
// registry-ID migration and were only ever exercised through the mocked
// anchor.Client in internal/mcp, leaving the real implementations -- their
// validation branches and, more importantly, their ABI argument order --
// uncovered here. These tests close that gap without needing a chain.

const testFrom = "0x1234567890abcdef1234567890abcdef12345678"

// prepareTestClient builds a client over the real ABI and a mock EVM
// backend whose defaults (nonce 0, 1 gwei, 100k gas) let the full
// build-unsigned-tx path run.
func prepareTestClient(t *testing.T) *client {
	t.Helper()
	raw := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
	c, ok := raw.(*client)
	if !ok {
		t.Fatalf("NewClient returned %T, want *client", raw)
	}
	return c
}

// decodeCalldata turns the 0x-prefixed Data field back into bytes.
func decodeCalldata(t *testing.T, dataHex string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(dataHex, "0x"))
	if err != nil {
		t.Fatalf("decode calldata %q: %v", dataHex, err)
	}
	return b
}

func TestPrepareUpdateRecordStatus_Validation(t *testing.T) {
	c := prepareTestClient(t)

	tests := []struct {
		name    string
		req     PrepareUpdateRecordStatusRequest
		wantErr error
		wantMsg string
	}{
		{
			name:    "missing from",
			req:     PrepareUpdateRecordStatusRequest{RegistryID: 1, RecordID: 1, Status: "Archived"},
			wantErr: apperrors.ErrMissingRequired,
			wantMsg: "from address",
		},
		{
			name:    "zero registry id",
			req:     PrepareUpdateRecordStatusRequest{From: testFrom, RecordID: 1, Status: "Archived"},
			wantErr: apperrors.ErrInvalidRegistryID,
			wantMsg: "registry_id must be > 0",
		},
		{
			name:    "zero record id",
			req:     PrepareUpdateRecordStatusRequest{From: testFrom, RegistryID: 1, Status: "Archived"},
			wantErr: apperrors.ErrInvalidRecordID,
			wantMsg: "record_id must be > 0",
		},
		{
			name:    "missing status",
			req:     PrepareUpdateRecordStatusRequest{From: testFrom, RegistryID: 1, RecordID: 1},
			wantErr: apperrors.ErrMissingRequired,
			wantMsg: "status is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.PrepareUpdateRecordStatus(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want errors.Is %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantMsg)
			}
			if !apperrors.IsInputError(err) {
				t.Errorf("caller-input rejection must classify as input error: %v", err)
			}
		})
	}
}

// TestPrepareUpdateRecordStatus_EncodesArgsInABIOrder pins the calldata
// against a fresh encode of the same arguments. The precompile takes
// (registryId, recordId, index, status) -- three adjacent uint64s that a
// transposition would silently survive, since any permutation still encodes.
func TestPrepareUpdateRecordStatus_EncodesArgsInABIOrder(t *testing.T) {
	c := prepareTestClient(t)

	const (
		registryID uint64 = 7
		recordID   uint64 = 42
		index      uint64 = 3
		status            = "Archived"
	)

	idx := index
	tx, err := c.PrepareUpdateRecordStatus(context.Background(), PrepareUpdateRecordStatusRequest{
		From:       testFrom,
		RegistryID: registryID,
		RecordID:   recordID,
		Index:      &idx,
		Status:     status,
	})
	if err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	want, err := c.parsedABI.Methods["updateRecordStatus"].EncodeArgs(registryID, recordID, index, status)
	if err != nil {
		t.Fatalf("encode expected args: %v", err)
	}
	if got := decodeCalldata(t, tx.Data); !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(want)) {
		t.Errorf("calldata mismatch\n got %x\nwant %x", got, want)
	}

	if tx.To != PrecompileAddress {
		t.Errorf("To = %q, want anchor precompile %q", tx.To, PrecompileAddress)
	}
	if tx.Value != "0" {
		t.Errorf("Value = %q, want 0 -- status updates must never carry value", tx.Value)
	}
	if tx.Type != 2 {
		t.Errorf("Type = %d, want 2 (EIP-1559 default)", tx.Type)
	}
	if tx.ChainID != 58887 {
		t.Errorf("ChainID = %d, want 58887", tx.ChainID)
	}
}

// TestPrepareUpdateRecordStatus_LegacyOptOut covers the PreferLegacy path,
// which the type-2 default otherwise leaves unexercised for this method.
func TestPrepareUpdateRecordStatus_LegacyOptOut(t *testing.T) {
	c := prepareTestClient(t)

	idx := uint64(1)
	tx, err := c.PrepareUpdateRecordStatus(context.Background(), PrepareUpdateRecordStatusRequest{
		From:         testFrom,
		RegistryID:   1,
		RecordID:     1,
		Index:        &idx,
		Status:       "Active",
		PreferLegacy: true,
	})
	if err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}
	if tx.Type != 0 {
		t.Errorf("Type = %d, want 0 under PreferLegacy", tx.Type)
	}
	if tx.MaxFeePerGas != "" {
		t.Errorf("MaxFeePerGas = %q, want empty on a legacy tx", tx.MaxFeePerGas)
	}
	if tx.GasPrice == "" {
		t.Error("GasPrice must be populated on a legacy tx")
	}
}

func TestPrepareRevokeRole_Validation(t *testing.T) {
	c := prepareTestClient(t)

	tests := []struct {
		name    string
		req     PrepareRevokeRoleRequest
		wantErr error
		wantMsg string
	}{
		{
			name:    "missing from",
			req:     PrepareRevokeRoleRequest{RegistryID: 1, Account: testFrom, Role: "editor"},
			wantErr: apperrors.ErrMissingRequired,
			wantMsg: "from address",
		},
		{
			name:    "missing account",
			req:     PrepareRevokeRoleRequest{From: testFrom, RegistryID: 1, Role: "editor"},
			wantErr: apperrors.ErrMissingRequired,
			wantMsg: "account address is required",
		},
		{
			name:    "missing role",
			req:     PrepareRevokeRoleRequest{From: testFrom, RegistryID: 1, Account: testFrom},
			wantErr: apperrors.ErrMissingRequired,
			wantMsg: "role is required",
		},
		{
			name:    "malformed account",
			req:     PrepareRevokeRoleRequest{From: testFrom, RegistryID: 1, Account: "not-an-address", Role: "editor"},
			wantErr: apperrors.ErrInvalidAddress,
			wantMsg: "not-an-address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.PrepareRevokeRole(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want errors.Is %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// TestPrepareRevokeRole_NormalizesChecksum asserts revokeRole strips a 0x
// prefix from the optional record-scoping checksum the same way addRecord and
// grantRole do. A revoke encoded with the 0x-prefixed form would not match the
// stored bare-hex grant, so the revoke would silently target nothing.
func TestPrepareRevokeRole_NormalizesChecksum(t *testing.T) {
	c := prepareTestClient(t)

	// A fixture checksum, not a credential; detect-secrets sees bare hex.
	const bare = "abc123def456" // pragma: allowlist secret
	req := PrepareRevokeRoleRequest{
		From:       testFrom,
		RegistryID: 9,
		Account:    "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Role:       "editor",
		Checksum:   "0x" + bare,
	}

	tx, err := c.PrepareRevokeRole(context.Background(), req)
	if err != nil {
		t.Fatalf("PrepareRevokeRole: %v", err)
	}

	account, err := defitypes.AddressFromHex(req.Account)
	if err != nil {
		t.Fatalf("parse account: %v", err)
	}
	want, err := c.parsedABI.Methods["revokeRole"].EncodeArgs(req.RegistryID, bare, account, req.Role)
	if err != nil {
		t.Fatalf("encode expected args: %v", err)
	}
	if got := decodeCalldata(t, tx.Data); hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("checksum not normalized: calldata\n got %x\nwant %x", got, want)
	}
}

// TestPrepareRoleStatus_PackErrors covers the EncodeArgs failure branch of
// both methods using an ABI whose argument lists do not match what the client
// encodes. TestPrepare_PackErrors covers the same branch for the older methods.
func TestPrepareRoleStatus_PackErrors(t *testing.T) {
	abiPath := writeTempABI(t, `[
	  {"type":"function","name":"updateRecordStatus","stateMutability":"nonpayable",
	   "inputs":[{"name":"registryId","type":"uint64"}],"outputs":[]},
	  {"type":"function","name":"revokeRole","stateMutability":"nonpayable",
	   "inputs":[{"name":"registryId","type":"uint64"}],"outputs":[]}
	]`)
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, abiPath, logging.New("error"))

	idx := uint64(1)
	_, err := c.PrepareUpdateRecordStatus(context.Background(), PrepareUpdateRecordStatusRequest{
		From:       testFrom,
		RegistryID: 1,
		RecordID:   1,
		Index:      &idx,
		Status:     "Archived",
	})
	if err == nil || !strings.Contains(err.Error(), "pack updateRecordStatus") {
		t.Errorf("want pack updateRecordStatus error, got %v", err)
	}

	_, err = c.PrepareRevokeRole(context.Background(), PrepareRevokeRoleRequest{
		From:       testFrom,
		RegistryID: 1,
		Account:    "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Role:       "editor",
	})
	if err == nil || !strings.Contains(err.Error(), "pack revokeRole") {
		t.Errorf("want pack revokeRole error, got %v", err)
	}
}

// TestPrepareRoleStatus_RequiresABI asserts both methods fail closed when the
// ABI never loaded, rather than emitting a transaction with empty calldata.
func TestPrepareRoleStatus_RequiresABI(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, "", logging.New("error"))

	if _, err := c.PrepareUpdateRecordStatus(context.Background(), PrepareUpdateRecordStatusRequest{
		From: testFrom, RegistryID: 1, RecordID: 1, Status: "Archived",
	}); err == nil {
		t.Error("PrepareUpdateRecordStatus must fail when the ABI is not loaded")
	}

	if _, err := c.PrepareRevokeRole(context.Background(), PrepareRevokeRoleRequest{
		From: testFrom, RegistryID: 1, Account: testFrom, Role: "editor",
	}); err == nil {
		t.Error("PrepareRevokeRole must fail when the ABI is not loaded")
	}
}
