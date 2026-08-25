// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package anchor_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// The rc17 registry-ID migration added updateRecordStatus and revokeRole.
// Every other write method has an on-chain round trip in write_integration_test.go;
// these two had none, so the ABI encoding was never validated against the live
// precompile -- an argument-order or checksum-normalization mistake would only
// have surfaced in production. These tests close that gap.
//
// Local run (needs a funded testnet wallet in ../../.chain_credentials.txt):
//
//	make test-integration

// roleGrantee is a burn address used purely as a role target. Roles are
// granted and revoked against it; it never needs to hold funds or sign.
const roleGrantee = "0x0000000000000000000000000000000000000002"

// submitAndConfirm signs, broadcasts, and waits for a successful receipt,
// failing the test on revert.
func submitAndConfirm(
	t *testing.T,
	utx *anchor.UnsignedTransaction,
	creds testCredentials,
	evmC evm.Client,
	label string,
) {
	t.Helper()

	signed := signUnsignedTx(t, utx, creds.PrivateKey)
	txHash, err := evmC.SendRawTransaction(context.Background(), signed)
	if err != nil {
		t.Fatalf("SendRawTransaction (%s): %v", label, err)
	}
	t.Logf("  %s tx hash: %s", label, txHash)

	receipt := waitForReceipt(t, evmC, txHash, receiptPollTimeout)
	if receipt.Status != "success" {
		t.Fatalf("%s reverted: status=%s", label, receipt.Status)
	}
	t.Logf("  %s mined in block %d (gas used: %d)", label, receipt.BlockNumber, receipt.GasUsed)
}

// findRecordByChecksum returns the record carrying the given checksum, or
// fails the test. Needed because updateRecordStatus is addressed by
// (recordID, index), which are assigned on chain rather than by the caller.
func findRecordByChecksum(
	t *testing.T,
	c anchor.Client,
	registryID uint64,
	checksum string,
) anchor.Record {
	t.Helper()

	resp, err := c.GetRecords(context.Background(), anchor.GetRecordsRequest{
		RegistryID: &registryID,
		Pagination: &anchor.PageRequest{Limit: 50},
	})
	if err != nil {
		t.Fatalf("GetRecords(registry=%d): %v", registryID, err)
	}
	for i := range resp.Records {
		if resp.Records[i].Checksum == checksum {
			return resp.Records[i]
		}
	}
	t.Fatalf("record with checksum %q not found in registry %d (%d records)",
		checksum, registryID, len(resp.Records))
	return anchor.Record{}
}

// TestIntegration_PrepareSignSubmit_UpdateRecordStatus walks the full
// lifecycle -- create registry, anchor a record, then flip its status --
// and reads the record back to confirm the new status actually landed.
// Reading back is the point: a transposed (registryId, recordId, index)
// argument triple still encodes and still mines, so only the observed
// state change proves the encoding is right.
func TestIntegration_PrepareSignSubmit_UpdateRecordStatus(t *testing.T) {
	creds := loadCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)
	ctx := context.Background()

	registryName := fmt.Sprintf("mcp-status-test-%d", time.Now().UnixNano())
	t.Logf("creating registry %q...", registryName)

	regUTX, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        creds.Address,
		Name:        registryName,
		Description: "Registry for updateRecordStatus e2e test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	submitAndConfirm(t, regUTX, creds, evmC, "addRegistry")

	regID := findRegistryIDByName(t, c, registryName)

	checksum := fmt.Sprintf("statustest%x", time.Now().UnixNano())
	t.Log("anchoring record with status Active...")
	recUTX, err := c.PrepareAddRecord(ctx, anchor.PrepareAddRecordRequest{
		From:         creds.Address,
		RegistryID:   regID,
		URI:          fmt.Sprintf("https://test.nvnmchain.io/status/%d", time.Now().UnixNano()),
		Checksum:     checksum,
		ChecksumAlgo: "sha256",
		Metadata:     `{"test":"update_record_status"}`,
		Status:       "Active",
	})
	if err != nil {
		t.Fatalf("PrepareAddRecord: %v", err)
	}
	submitAndConfirm(t, recUTX, creds, evmC, "addRecord")

	rec := findRecordByChecksum(t, c, regID, checksum)
	t.Logf("  record anchored: recordID=%d index=%d status=%q", rec.RecordID, rec.Index, rec.Status)
	if rec.Status != "Active" {
		t.Fatalf("precondition failed: new record status = %q, want Active", rec.Status)
	}

	t.Log("preparing updateRecordStatus -> Archived...")
	statusUTX, err := c.PrepareUpdateRecordStatus(ctx, anchor.PrepareUpdateRecordStatusRequest{
		From:       creds.Address,
		RegistryID: regID,
		RecordID:   rec.RecordID,
		Index:      rec.Index,
		Status:     "Archived",
	})
	if err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}
	if statusUTX.To != anchor.PrecompileAddress {
		t.Errorf("To = %q, want anchor precompile", statusUTX.To)
	}
	submitAndConfirm(t, statusUTX, creds, evmC, "updateRecordStatus")

	updated := findRecordByChecksum(t, c, regID, checksum)
	if updated.Status != "Archived" {
		t.Errorf("status = %q after updateRecordStatus, want Archived", updated.Status)
	}
	if updated.RecordID != rec.RecordID {
		t.Errorf("RecordID changed from %d to %d; status update must not re-key the record",
			rec.RecordID, updated.RecordID)
	}
	t.Logf("  status confirmed on chain: %q", updated.Status)
}

// TestIntegration_PrepareSignSubmit_RevokeRole grants a role and then revokes
// it against the live precompile. Both halves are required: revoking a role
// that was never granted can succeed vacuously, so the grant establishes the
// state that makes the revoke meaningful.
func TestIntegration_PrepareSignSubmit_RevokeRole(t *testing.T) {
	creds := loadCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)
	ctx := context.Background()

	registryName := fmt.Sprintf("mcp-revoke-test-%d", time.Now().UnixNano())
	t.Logf("creating registry %q...", registryName)

	regUTX, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        creds.Address,
		Name:        registryName,
		Description: "Registry for revokeRole e2e test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	submitAndConfirm(t, regUTX, creds, evmC, "addRegistry")

	regID := findRegistryIDByName(t, c, registryName)

	t.Log("granting editor role...")
	grantUTX, err := c.PrepareGrantRole(ctx, anchor.PrepareGrantRoleRequest{
		From:       creds.Address,
		RegistryID: regID,
		Account:    roleGrantee,
		Role:       "editor",
	})
	if err != nil {
		t.Fatalf("PrepareGrantRole: %v", err)
	}
	submitAndConfirm(t, grantUTX, creds, evmC, "grantRole")

	t.Log("preparing revokeRole...")
	revokeUTX, err := c.PrepareRevokeRole(ctx, anchor.PrepareRevokeRoleRequest{
		From:       creds.Address,
		RegistryID: regID,
		Account:    roleGrantee,
		Role:       "editor",
	})
	if err != nil {
		t.Fatalf("PrepareRevokeRole: %v", err)
	}
	if revokeUTX.To != anchor.PrecompileAddress {
		t.Errorf("To = %q, want anchor precompile", revokeUTX.To)
	}
	if revokeUTX.Value != "0" {
		t.Errorf("Value = %q, want 0", revokeUTX.Value)
	}
	submitAndConfirm(t, revokeUTX, creds, evmC, "revokeRole")
}

// TestIntegration_PrepareRevokeRole_ChecksumScoped exercises the optional
// record-scoping checksum on the grant/revoke pair. The client strips a 0x
// prefix before encoding; if that normalization regressed, the revoke would
// encode a checksum that does not match the stored grant and the precompile
// would reject it -- which is why this passes the 0x-prefixed form.
func TestIntegration_PrepareRevokeRole_ChecksumScoped(t *testing.T) {
	creds := loadCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)
	ctx := context.Background()

	registryName := fmt.Sprintf("mcp-revoke-cs-test-%d", time.Now().UnixNano())
	regUTX, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        creds.Address,
		Name:        registryName,
		Description: "Registry for checksum-scoped revokeRole e2e test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	submitAndConfirm(t, regUTX, creds, evmC, "addRegistry")

	regID := findRegistryIDByName(t, c, registryName)

	// The precompile scopes a role to an existing record, so the record has
	// to be anchored before the grant: scoping to a checksum that was never
	// anchored fails gas estimation with "record with checksum ... does not
	// exist in registry". Anchored without the 0x prefix so the grant and
	// revoke below, which pass the prefixed form, exercise the client's
	// normalization against a stored value that lacks it.
	checksum := fmt.Sprintf("%064x", time.Now().UnixNano())
	recUTX, err := c.PrepareAddRecord(ctx, anchor.PrepareAddRecordRequest{
		From:         creds.Address,
		RegistryID:   regID,
		URI:          fmt.Sprintf("https://test.nvnmchain.io/revoke-cs/%d", time.Now().UnixNano()),
		Checksum:     checksum,
		ChecksumAlgo: "sha256",
		Metadata:     `{"test":"revoke_role_checksum_scoped"}`,
		Status:       "Active",
	})
	if err != nil {
		t.Fatalf("PrepareAddRecord: %v", err)
	}
	submitAndConfirm(t, recUTX, creds, evmC, "addRecord")

	rec := findRecordByChecksum(t, c, regID, checksum)
	t.Logf("  record anchored for scoping: recordID=%d index=%d", rec.RecordID, rec.Index)

	grantUTX, err := c.PrepareGrantRole(ctx, anchor.PrepareGrantRoleRequest{
		From:       creds.Address,
		RegistryID: regID,
		Account:    roleGrantee,
		Role:       "editor",
		Checksum:   "0x" + checksum,
	})
	if err != nil {
		t.Fatalf("PrepareGrantRole (checksum-scoped): %v", err)
	}
	submitAndConfirm(t, grantUTX, creds, evmC, "grantRole(checksum)")

	revokeUTX, err := c.PrepareRevokeRole(ctx, anchor.PrepareRevokeRoleRequest{
		From:       creds.Address,
		RegistryID: regID,
		Account:    roleGrantee,
		Role:       "editor",
		Checksum:   "0x" + checksum,
	})
	if err != nil {
		t.Fatalf("PrepareRevokeRole (checksum-scoped): %v", err)
	}
	submitAndConfirm(t, revokeUTX, creds, evmC, "revokeRole(checksum)")
}
