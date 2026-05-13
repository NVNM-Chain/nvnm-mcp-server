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

// Phase 8.4 integration tests. Each test reads NVNM_TEST_PRIVATE_KEY
// from the environment and skips when unset, so an integration test
// run without testnet credentials does not fail loud.
//
// Local run:
//
//	set -a; source .env; set +a
//	make test-integration
//
// Each test broadcasts a real anchor_prepare_add_registry transaction
// against the configured testnet (manveniam-1, chain ID 58887). The
// registry name is timestamped so each run is idempotent against the
// on-chain unique-name constraint.

const phase84ReceiptTimeout = 90 * time.Second

// envCredentials reads the test wallet from NVNM_TEST_PRIVATE_KEY (set
// in the gitignored .env). Returns the parsed key and the derived
// address. Skips the test if the env var is unset.
func envCredentials(t *testing.T) (*defiwallet.PrivateKey, defitypes.Address) {
	t.Helper()
	raw := os.Getenv("NVNM_TEST_PRIVATE_KEY")
	if raw == "" {
		t.Skip("NVNM_TEST_PRIVATE_KEY not set; skipping testnet round-trip")
	}
	raw = strings.TrimPrefix(raw, "0x")
	keyBytes, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode NVNM_TEST_PRIVATE_KEY: %v", err)
	}
	priv := defiwallet.NewKeyFromBytes(keyBytes)
	return priv, priv.Address()
}

// TestIntegration_PrepareAddRegistry_EIP1559_RoundTrip exercises the
// default (Phase 8.4+) type-2 path against testnet end-to-end: prepare
// the unsigned tx, sign locally with go-ethereum, broadcast via
// SendRawTransaction, poll for receipt, assert success.
func TestIntegration_PrepareAddRegistry_EIP1559_RoundTrip(t *testing.T) {
	priv, addr := envCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), phase84ReceiptTimeout)
	defer cancel()

	regName := fmt.Sprintf("p8.4-eip1559-%d", time.Now().UnixNano())
	tx, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        evm.AddressHex(addr),
		Name:        regName,
		Description: "Phase 8.4 EIP-1559 round-trip integration test",
		Metadata:    `{"phase":"8.4","tx_type":"2"}`,
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	if tx.Type != 2 {
		t.Errorf("Type = %d, want 2 (default EIP-1559)", tx.Type)
	}
	if tx.MaxFeePerGas == "" {
		t.Error("MaxFeePerGas must be populated on type-2 tx")
	}
	if tx.MaxPriorityFeePerGas == "" {
		t.Error("MaxPriorityFeePerGas must be populated on type-2 tx")
	}

	signedHex := signUnsignedTx(t, tx, priv)
	txHash, err := evmC.SendRawTransaction(ctx, signedHex)
	if err != nil {
		t.Fatalf("SendRawTransaction: %v", err)
	}
	t.Logf("broadcast type-2 tx_hash=%s nonce=%d max_fee_per_gas=%s",
		txHash, tx.Nonce, tx.MaxFeePerGas)

	receipt := waitForReceipt(t, evmC, txHash, phase84ReceiptTimeout)
	if receipt.Status != "success" {
		t.Errorf("receipt status = %q, want success", receipt.Status)
	}
}

// TestIntegration_PrepareAddRegistry_Legacy_RoundTrip exercises the
// PreferLegacy opt-out (type-0) path end-to-end. Same flow as the
// EIP-1559 test but with PreferLegacy=true on the request.
func TestIntegration_PrepareAddRegistry_Legacy_RoundTrip(t *testing.T) {
	priv, addr := envCredentials(t)
	c := integrationClient(t)
	evmC := integrationEVMClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), phase84ReceiptTimeout)
	defer cancel()

	regName := fmt.Sprintf("p8.4-legacy-%d", time.Now().UnixNano())
	tx, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:         evm.AddressHex(addr),
		Name:         regName,
		Description:  "Phase 8.4 legacy-fallback round-trip integration test",
		Metadata:     `{"phase":"8.4","tx_type":"0"}`,
		PreferLegacy: true,
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	if tx.Type != 0 {
		t.Errorf("Type = %d, want 0 (legacy)", tx.Type)
	}
	if tx.MaxFeePerGas != "" {
		t.Errorf("MaxFeePerGas should be empty for legacy tx, got %q", tx.MaxFeePerGas)
	}

	signedHex := signUnsignedTx(t, tx, priv)
	txHash, err := evmC.SendRawTransaction(ctx, signedHex)
	if err != nil {
		t.Fatalf("SendRawTransaction: %v", err)
	}
	t.Logf("broadcast type-0 tx_hash=%s nonce=%d gas_price=%s",
		txHash, tx.Nonce, tx.GasPrice)

	receipt := waitForReceipt(t, evmC, txHash, phase84ReceiptTimeout)
	if receipt.Status != "success" {
		t.Errorf("receipt status = %q, want success", receipt.Status)
	}
}
