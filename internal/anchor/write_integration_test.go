//go:build integration

package anchor_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

const credentialsPath = "../../.chain_credentials.txt"

// receiptPollTimeout bounds how long each test waits for a receipt
// to appear. The testnet RPC occasionally takes >30s to surface a
// receipt for a tx we know is on-chain (slow comet-side indexing on
// busy blocks); 60s covers the observed worst case with margin.
const receiptPollTimeout = 60 * time.Second

type testCredentials struct {
	Address    string
	PrivateKey *defiwallet.PrivateKey
}

func loadCredentials(t *testing.T) testCredentials {
	t.Helper()

	data, err := os.ReadFile(credentialsPath) //nolint:gosec // test fixture
	if err != nil {
		t.Skipf("credentials file not found (%s): %v", credentialsPath, err)
	}

	var address, keyHex string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Address:"); ok {
			address = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "PrivateKey:"); ok {
			keyHex = strings.TrimSpace(after)
		}
	}
	if address == "" || keyHex == "" {
		t.Fatal("credentials file missing Address or PrivateKey")
	}

	keyHex = strings.TrimPrefix(keyHex, "0x")
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("invalid private key hex: %v", err)
	}
	privKey := defiwallet.NewKeyFromBytes(keyBytes)

	return testCredentials{Address: address, PrivateKey: privKey}
}

func integrationEVMClient(t *testing.T) evm.Client {
	t.Helper()
	// Reuse the shared resilient wrapper so prepare-sign-submit flows
	// get production-shaped retry coverage on the comet-receipts race.
	return integrationResilientEVMClient(t)
}

// signUnsignedTx deserializes an UnsignedTransaction, signs it with
// the given private key, and returns the signed transaction as
// 0x-prefixed hex. Handles type-0 and type-2 transactions uniformly.
func signUnsignedTx(
	t *testing.T,
	utx *anchor.UnsignedTransaction,
	key *defiwallet.PrivateKey,
) string {
	t.Helper()

	rawHex := strings.TrimPrefix(utx.RawTx, "0x")
	txBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("decode raw tx hex: %v", err)
	}

	tx := defitypes.NewTransaction()
	if _, err := tx.DecodeRLP(txBytes); err != nil {
		t.Fatalf("unmarshal unsigned tx: %v", err)
	}
	if utx.ChainID < 0 {
		t.Fatalf("negative chain ID %d", utx.ChainID)
	}
	tx.SetChainID(uint64(utx.ChainID))

	if err := key.SignTransaction(context.Background(), tx); err != nil {
		t.Fatalf("sign tx: %v", err)
	}

	signedBytes, err := tx.Raw()
	if err != nil {
		t.Fatalf("marshal signed tx: %v", err)
	}

	return "0x" + hex.EncodeToString(signedBytes)
}

// waitForReceipt polls for a transaction receipt until it appears or the
// timeout expires. Returns the receipt status or fatals on timeout.
func waitForReceipt(
	t *testing.T,
	evmClient evm.Client,
	txHash string,
	timeout time.Duration,
) *evm.NormalizedReceipt {
	t.Helper()

	hash, err := defitypes.HashFromHex(txHash, defitypes.PadNone)
	if err != nil {
		t.Fatalf("invalid tx hash %s: %v", txHash, err)
	}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		receipt, err := evmClient.TransactionReceipt(context.Background(), hash)
		if err != nil {
			t.Logf("  receipt not yet available: %v", err)
			continue
		}

		t.Logf("  status=%s gasUsed=%d block=%d", receipt.Status, receipt.GasUsed, receipt.BlockNumber)
		return receipt
	}

	t.Fatalf("timed out after %s waiting for receipt of %s", timeout, txHash)
	return nil
}

func TestIntegration_PrepareSignSubmit_AddRegistry(t *testing.T) {
	creds := loadCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)
	ctx := context.Background()

	registryName := fmt.Sprintf("mcp-e2e-test-%d", time.Now().UnixNano())

	// Step 1: Prepare
	t.Log("preparing addRegistry transaction...")
	utx, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        creds.Address,
		Name:        registryName,
		Description: "End-to-end integration test registry",
		Metadata:    `{"created_by":"integration_test"}`,
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}

	if utx.ChainID != testChainID {
		t.Errorf("ChainID = %d, want %d", utx.ChainID, testChainID)
	}
	if utx.Gas == 0 {
		t.Error("Gas should be > 0")
	}
	t.Logf("  nonce=%d gas=%d gasPrice=%s", utx.Nonce, utx.Gas, utx.GasPrice)

	// Step 2: Sign
	t.Log("signing transaction...")
	signedHex := signUnsignedTx(t, utx, creds.PrivateKey)
	t.Logf("  signed tx length: %d hex chars", len(signedHex))

	// Step 3: Submit
	t.Log("submitting signed transaction...")
	txHash, err := evmC.SendRawTransaction(ctx, signedHex)
	if err != nil {
		t.Fatalf("SendRawTransaction: %v", err)
	}
	t.Logf("  tx hash: %s", txHash)

	// Step 4: Wait for receipt
	receipt := waitForReceipt(t, evmC, txHash, receiptPollTimeout)
	if receipt.Status != "success" {
		t.Fatalf("transaction reverted: status=%s", receipt.Status)
	}
	t.Logf("  transaction mined in block %d", receipt.BlockNumber)

	// Step 5: Verify the registry was created
	t.Log("verifying registry on chain...")
	reg, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{Name: &registryName})
	if err != nil {
		t.Fatalf("GetRegistry(name=%q): %v", registryName, err)
	}

	if reg.Name != registryName {
		t.Errorf("Name = %q, want %q", reg.Name, registryName)
	}
	if reg.Description != "End-to-end integration test registry" {
		t.Errorf("Description = %q", reg.Description)
	}
	if reg.Creator == "" {
		t.Error("Creator should not be empty")
	}
	if reg.ID == 0 {
		t.Error("ID should be > 0")
	}
	t.Logf("  registry confirmed: id=%d name=%q creator=%s", reg.ID, reg.Name, reg.Creator)
}

func TestIntegration_PrepareSignSubmit_AddRecord(t *testing.T) {
	creds := loadCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)
	ctx := context.Background()

	// Create a fresh registry first
	registryName := fmt.Sprintf("mcp-record-test-%d", time.Now().UnixNano())
	t.Logf("creating registry %q...", registryName)

	regUTX, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        creds.Address,
		Name:        registryName,
		Description: "Registry for addRecord e2e test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}

	regSigned := signUnsignedTx(t, regUTX, creds.PrivateKey)
	regTxHash, err := evmC.SendRawTransaction(ctx, regSigned)
	if err != nil {
		t.Fatalf("SendRawTransaction (addRegistry): %v", err)
	}

	regReceipt := waitForReceipt(t, evmC, regTxHash, receiptPollTimeout)
	if regReceipt.Status != "success" {
		t.Fatalf("addRegistry reverted: status=%s", regReceipt.Status)
	}
	t.Logf("  registry created in block %d", regReceipt.BlockNumber)

	// Prepare and submit an addRecord
	testChecksum := fmt.Sprintf("sha256:e2etest%x", time.Now().UnixNano())
	testURI := fmt.Sprintf("https://test.inveniam.io/docs/%d", time.Now().UnixNano())

	t.Log("preparing addRecord transaction...")
	recUTX, err := c.PrepareAddRecord(ctx, anchor.PrepareAddRecordRequest{
		From:         creds.Address,
		Registry:     registryName,
		URI:          testURI,
		Checksum:     testChecksum,
		ChecksumAlgo: "sha256",
		Metadata:     `{"test":"e2e_record"}`,
	})
	if err != nil {
		t.Fatalf("PrepareAddRecord: %v", err)
	}
	t.Logf("  nonce=%d gas=%d", recUTX.Nonce, recUTX.Gas)

	recSigned := signUnsignedTx(t, recUTX, creds.PrivateKey)
	recTxHash, err := evmC.SendRawTransaction(ctx, recSigned)
	if err != nil {
		t.Fatalf("SendRawTransaction (addRecord): %v", err)
	}
	t.Logf("  record tx hash: %s", recTxHash)

	recReceipt := waitForReceipt(t, evmC, recTxHash, receiptPollTimeout)
	if recReceipt.Status != "success" {
		t.Fatalf("addRecord reverted: status=%s", recReceipt.Status)
	}
	t.Logf("  record mined in block %d", recReceipt.BlockNumber)

	// Verify the record
	t.Log("verifying record on chain...")
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &registryName,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(resp.Records) == 0 {
		t.Fatal("expected at least one record after addRecord")
	}

	found := false
	for _, rec := range resp.Records {
		if rec.Checksum == testChecksum {
			found = true
			if rec.URI != testURI {
				t.Errorf("URI = %q, want %q", rec.URI, testURI)
			}
			t.Logf("  record confirmed: recordID=%d checksum=%s uri=%s",
				rec.RecordID, rec.Checksum, rec.URI)
			break
		}
	}
	if !found {
		t.Errorf("record with checksum %q not found in registry %q", testChecksum, registryName)
	}
}

func TestIntegration_PrepareSignSubmit_GrantRole(t *testing.T) {
	creds := loadCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)
	ctx := context.Background()

	registryName := fmt.Sprintf("mcp-grant-test-%d", time.Now().UnixNano())
	t.Logf("creating registry %q for grantRole test...", registryName)

	regUTX, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        creds.Address,
		Name:        registryName,
		Description: "Registry for grantRole e2e test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}

	regSigned := signUnsignedTx(t, regUTX, creds.PrivateKey)
	regTxHash, err := evmC.SendRawTransaction(ctx, regSigned)
	if err != nil {
		t.Fatalf("SendRawTransaction (addRegistry): %v", err)
	}
	regReceipt := waitForReceipt(t, evmC, regTxHash, receiptPollTimeout)
	if regReceipt.Status != "success" {
		t.Fatalf("addRegistry reverted: status=%s", regReceipt.Status)
	}
	t.Logf("  registry created in block %d", regReceipt.BlockNumber)

	reg, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{Name: &registryName})
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}

	granteeAddr := "0x0000000000000000000000000000000000000001"

	t.Log("preparing grantRole transaction...")
	grantUTX, err := c.PrepareGrantRole(ctx, anchor.PrepareGrantRoleRequest{
		From:       creds.Address,
		RegistryID: reg.ID,
		Account:    granteeAddr,
		Role:       "editor",
	})
	if err != nil {
		t.Fatalf("PrepareGrantRole: %v", err)
	}
	t.Logf("  nonce=%d gas=%d", grantUTX.Nonce, grantUTX.Gas)

	grantSigned := signUnsignedTx(t, grantUTX, creds.PrivateKey)
	grantTxHash, err := evmC.SendRawTransaction(ctx, grantSigned)
	if err != nil {
		t.Fatalf("SendRawTransaction (grantRole): %v", err)
	}
	t.Logf("  grant tx hash: %s", grantTxHash)

	grantReceipt := waitForReceipt(t, evmC, grantTxHash, receiptPollTimeout)
	if grantReceipt.Status != "success" {
		t.Fatalf("grantRole reverted: status=%s", grantReceipt.Status)
	}
	t.Logf("  grantRole mined in block %d (gas used: %d)",
		grantReceipt.BlockNumber, grantReceipt.GasUsed)
}
