// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
)

// --- loadCredentials ---

func writeCreds(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "creds.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCredentials_HappyPath(t *testing.T) {
	keyHex := "0x" + strings.Repeat("11", 32)
	path := writeCreds(t,
		"Address: 0x00000000000000000000000000000000000000AA\n"+
			"PrivateKey: "+keyHex+"\n")
	creds, err := loadCredentials(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.address != "0x00000000000000000000000000000000000000AA" {
		t.Errorf("address = %q", creds.address)
	}
	if creds.privateKey == nil {
		t.Error("expected a parsed private key")
	}
}

func TestLoadCredentials_MissingFields(t *testing.T) {
	path := writeCreds(t, "Address: 0xAA\n") // no PrivateKey line
	_, err := loadCredentials(path)
	if !errors.Is(err, errMissingCredField) {
		t.Fatalf("err = %v, want errMissingCredField", err)
	}
}

func TestLoadCredentials_InvalidKeyHex(t *testing.T) {
	path := writeCreds(t, "Address: 0xAA\nPrivateKey: 0xZZZZ\n")
	if _, err := loadCredentials(path); err == nil {
		t.Fatal("expected error for invalid private key hex, got nil")
	}
}

func TestLoadCredentials_UnreadableFile(t *testing.T) {
	if _, err := loadCredentials(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// --- signTx ---

func testUnsignedTx(t *testing.T, chainID int64) *anchor.UnsignedTransaction {
	t.Helper()
	to, err := defitypes.AddressFromHex(anchor.PrecompileAddress)
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	tx := defitypes.NewTransaction().
		SetType(defitypes.LegacyTxType).
		SetTo(to).
		SetValue(big.NewInt(0)).
		SetGasLimit(100000).
		SetGasPrice(big.NewInt(1_000_000_000)).
		SetInput([]byte{0x01, 0x02}).
		SetNonce(0)
	raw, err := tx.Raw()
	if err != nil {
		t.Fatalf("serialize unsigned tx: %v", err)
	}
	return &anchor.UnsignedTransaction{
		RawTx:   "0x" + hex.EncodeToString(raw),
		ChainID: chainID,
	}
}

func testPrivateKey(t *testing.T) *defiwallet.PrivateKey {
	t.Helper()
	keyBytes, err := hex.DecodeString(strings.Repeat("22", 32))
	if err != nil {
		t.Fatal(err)
	}
	return defiwallet.NewKeyFromBytes(keyBytes)
}

func TestSignTx_HappyPath(t *testing.T) {
	utx := testUnsignedTx(t, 787111)
	signed, err := signTx(context.Background(), utx, testPrivateKey(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(signed, "0x") {
		t.Fatalf("signed tx must be 0x-prefixed hex, got %q", signed)
	}
	if _, decErr := hex.DecodeString(strings.TrimPrefix(signed, "0x")); decErr != nil {
		t.Fatalf("signed tx is not valid hex: %v", decErr)
	}
	if signed == utx.RawTx {
		t.Fatal("signed tx must differ from the unsigned input")
	}
}

func TestSignTx_InvalidRawHex(t *testing.T) {
	utx := &anchor.UnsignedTransaction{RawTx: "0xZZZZ", ChainID: 1}
	if _, err := signTx(context.Background(), utx, testPrivateKey(t)); err == nil {
		t.Fatal("expected error for invalid raw tx hex, got nil")
	}
}

func TestSignTx_UndecodableRLP(t *testing.T) {
	utx := &anchor.UnsignedTransaction{RawTx: "0xdeadbeef", ChainID: 1}
	if _, err := signTx(context.Background(), utx, testPrivateKey(t)); err == nil {
		t.Fatal("expected error for undecodable RLP payload, got nil")
	}
}

func TestSignTx_NegativeChainID(t *testing.T) {
	utx := testUnsignedTx(t, -1)
	_, err := signTx(context.Background(), utx, testPrivateKey(t))
	if !errors.Is(err, errNegativeChainID) {
		t.Fatalf("err = %v, want errNegativeChainID", err)
	}
}

// --- strPtr ---

func TestStrPtr(t *testing.T) {
	p := strPtr("hello")
	if p == nil || *p != "hello" {
		t.Fatalf("strPtr = %v", p)
	}
}
