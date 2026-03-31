//go:build integration

package anchor_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

const credentialsPath = "../../.chain_credentials.txt"

type testCredentials struct {
	Address    string
	PrivateKey *ecdsa.PrivateKey
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
	privKey, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		t.Fatalf("invalid private key: %v", err)
	}

	return testCredentials{Address: address, PrivateKey: privKey}
}

func integrationEVMClient(t *testing.T) evm.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()

	c, err := evm.NewClient(ctx, testRPCURL, testConnectTimeout)
	if err != nil {
		t.Fatalf("failed to connect to %s: %v", testRPCURL, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// signUnsignedTx deserializes an UnsignedTransaction, signs it with the given
// private key using EIP-155, and returns the signed transaction as 0x-prefixed hex.
func signUnsignedTx(
	t *testing.T,
	utx *anchor.UnsignedTransaction,
	key *ecdsa.PrivateKey,
) string {
	t.Helper()

	rawHex := strings.TrimPrefix(utx.RawTx, "0x")
	txBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("decode raw tx hex: %v", err)
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		t.Fatalf("unmarshal unsigned tx: %v", err)
	}

	signer := types.NewEIP155Signer(big.NewInt(utx.ChainID))
	signedTx, err := types.SignTx(tx, signer, key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}

	signedBytes, err := signedTx.MarshalBinary()
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

	hash := common.HexToHash(txHash)
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
	receipt := waitForReceipt(t, evmC, txHash, 30*time.Second)
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

	regReceipt := waitForReceipt(t, evmC, regTxHash, 30*time.Second)
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

	recReceipt := waitForReceipt(t, evmC, recTxHash, 30*time.Second)
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
