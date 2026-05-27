// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package mcp

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

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

const (
	rtRPCURL          = "https://evm.testnet.nvnmchain.io"
	rtChainID         = 787111
	rtABIPath         = "../../abi/anchoring.json"
	rtCredentialsPath = "../../.chain_credentials.txt"
	// rtReceiptTimeout bounds the receipt poll. The testnet RPC can
	// take >30s to surface a receipt for a tx already on-chain (slow
	// comet-side indexing on busy blocks); 90s covers the observed
	// worst case with margin.
	rtReceiptTimeout = 90 * time.Second
)

// TestIntegration_EthAccountRoundTrip exercises the full eth_account
// (local / headless signer) write path against testnet and verifies
// that the wallet_status tool reflects the post-broadcast nonce.
//
// Round-trip: wallet_status (before) -> anchor_prepare_add_registry ->
// sign with the local private key -> evm_send_raw_transaction ->
// receipt -> wallet_status (after). This is the Phase 8.12 gate
// "manual testnet round-trip: prepare -> sign (eth_account) ->
// broadcast -> receipt -> wallet_status reflects new nonce", codified
// as a repeatable integration test rather than a one-off probe.
//
// It drives the real wallet_status tool core (makeWalletStatusHandler)
// with a claims-free context -- the stdio-equivalent path, where
// requireRole is a documented no-op (no auth context, no enforcement).
func TestIntegration_EthAccountRoundTrip(t *testing.T) {
	addr, key := rtLoadCredentials(t)
	ctx := context.Background()

	evmClient, err := evm.NewClient(ctx, rtRPCURL, 15*time.Second)
	if err != nil {
		t.Fatalf("evm.NewClient: %v", err)
	}
	defer evmClient.Close()

	// Minimal config: makeWalletStatusHandler only reads ChainID (for
	// the output) and ChainEnvironment (for env-aware token naming).
	cfg := &config.Config{ChainID: rtChainID, ChainEnvironment: config.EnvTestnet}
	walletStatus := makeWalletStatusHandler(evmClient, cfg)

	// --- wallet_status BEFORE ---
	_, before, err := walletStatus(ctx, nil, walletStatusInput{Address: addr})
	if err != nil {
		t.Fatalf("wallet_status (before): %v", err)
	}
	t.Logf("before: nonce=%d status=%s balance=%s", before.Nonce, before.Status, before.BalanceHuman)
	if before.Status == WalletStatusUnfunded {
		t.Skipf("test wallet %s is unfunded; fund it before running the round-trip", addr)
	}

	// --- prepare (anchor_prepare_add_registry, unique name) ---
	logger := logging.NewText("warn")
	anchorClient := anchor.NewClient(evmClient, anchor.PrecompileAddress, rtChainID, rtABIPath, logger)
	registryName := fmt.Sprintf("mcp-eth-account-rt-%d", time.Now().UnixNano())
	utx, err := anchorClient.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
		From:        addr,
		Name:        registryName,
		Description: "Phase 8.12 eth_account round-trip verification",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	t.Logf("prepared: type=%d nonce=%d gas=%d", utx.Type, utx.Nonce, utx.Gas)

	// --- sign (eth_account / local headless signer, raw_tx path) ---
	signedHex := rtSignUnsignedTx(t, utx, key)

	// --- broadcast ---
	txHash, err := evmClient.SendRawTransaction(ctx, signedHex)
	if err != nil {
		t.Fatalf("SendRawTransaction: %v", err)
	}
	t.Logf("broadcast: tx=%s", txHash)

	// --- receipt ---
	receipt := rtWaitForReceipt(t, evmClient, txHash, rtReceiptTimeout)
	if receipt.Status != "success" {
		t.Fatalf("transaction reverted: status=%s", receipt.Status)
	}
	t.Logf("mined: block=%d gasUsed=%d", receipt.BlockNumber, receipt.GasUsed)

	// --- wallet_status AFTER ---
	_, after, err := walletStatus(ctx, nil, walletStatusInput{Address: addr})
	if err != nil {
		t.Fatalf("wallet_status (after): %v", err)
	}
	t.Logf("after: nonce=%d status=%s balance=%s", after.Nonce, after.Status, after.BalanceHuman)

	// --- assertions: wallet_status reflects the new nonce ---
	if after.Nonce != before.Nonce+1 {
		t.Errorf("nonce did not advance by exactly 1: before=%d after=%d "+
			"(a shared test key used concurrently can cause this)", before.Nonce, after.Nonce)
	}
	if !after.HasSentTx {
		t.Error("has_sent_tx = false after a successful broadcast")
	}
	if after.Status != WalletStatusFundedActive {
		t.Errorf("status = %q after broadcast, want %q", after.Status, WalletStatusFundedActive)
	}
	wantToken := config.NamingFor(config.EnvTestnet).Wrapped
	if !strings.HasSuffix(after.BalanceHuman, wantToken) {
		t.Errorf("balance_human = %q, want suffix %q", after.BalanceHuman, wantToken)
	}
	if len(after.NextActions) == 0 {
		t.Error("next_actions empty for funded_active status")
	}
}

// rtLoadCredentials reads the Address + PrivateKey lines from the
// git-ignored credentials file. Skips the test (not fails) when the
// file is absent so the suite is runnable without testnet creds.
func rtLoadCredentials(t *testing.T) (address string, key *defiwallet.PrivateKey) {
	t.Helper()

	data, err := os.ReadFile(rtCredentialsPath)
	if err != nil {
		t.Skipf("credentials file not found (%s): %v", rtCredentialsPath, err)
	}

	var keyHex string
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

	keyBytes, err := hex.DecodeString(strings.TrimPrefix(keyHex, "0x"))
	if err != nil {
		t.Fatalf("invalid private key hex: %v", err)
	}
	return address, defiwallet.NewKeyFromBytes(keyBytes)
}

// rtSignUnsignedTx decodes an UnsignedTransaction's RLP raw_tx, signs
// it with the local private key, and returns 0x-prefixed signed hex.
// This is the eth_account signing path -- the headless counterpart to
// the MetaMask / EIP-1193 wallet_tx_request path.
func rtSignUnsignedTx(t *testing.T, utx *anchor.UnsignedTransaction, key *defiwallet.PrivateKey) string {
	t.Helper()

	txBytes, err := hex.DecodeString(strings.TrimPrefix(utx.RawTx, "0x"))
	if err != nil {
		t.Fatalf("decode raw tx hex: %v", err)
	}
	tx := defitypes.NewTransaction()
	if _, decErr := tx.DecodeRLP(txBytes); decErr != nil {
		t.Fatalf("unmarshal unsigned tx: %v", decErr)
	}
	if utx.ChainID < 0 {
		t.Fatalf("negative chain ID %d", utx.ChainID)
	}
	tx.SetChainID(uint64(utx.ChainID))

	if signErr := key.SignTransaction(context.Background(), tx); signErr != nil {
		t.Fatalf("sign tx: %v", signErr)
	}
	signedBytes, err := tx.Raw()
	if err != nil {
		t.Fatalf("marshal signed tx: %v", err)
	}
	return "0x" + hex.EncodeToString(signedBytes)
}

// rtWaitForReceipt polls for a transaction receipt until it appears or
// the timeout expires. Fatals on timeout.
func rtWaitForReceipt(
	t *testing.T, evmClient evm.Client, txHash string, timeout time.Duration,
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
		return receipt
	}
	t.Fatalf("timed out after %s waiting for receipt of %s", timeout, txHash)
	return nil
}
