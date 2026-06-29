// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

const anchorHex = "0x0000000000000000000000000000000000000A00"

// captureClient implements evm.Client by embedding the interface (nil) and
// overriding only SendRawTransaction, the sole method the handler calls.
type captureClient struct {
	evm.Client
	gotHex string
	called bool
	txHash string
}

func (c *captureClient) SendRawTransaction(_ context.Context, signedTxHex string) (string, error) {
	c.called = true
	c.gotHex = signedTxHex
	if c.txHash == "" {
		c.txHash = "0xhash"
	}
	return c.txHash, nil
}

// signedTxTo builds a signed dynamic-fee tx to `to` and returns its canonical hex.
func signedTxTo(t *testing.T, key *wallet.PrivateKey, to defitypes.Address) string {
	t.Helper()
	tx := defitypes.NewTransaction().
		SetType(defitypes.DynamicFeeTxType).
		SetChainID(787111).SetNonce(0).SetGasLimit(21000).
		SetMaxFeePerGas(big.NewInt(2_000_000_000)).
		SetMaxPriorityFeePerGas(big.NewInt(1_000_000_000)).
		SetTo(to).SetValue(big.NewInt(0)).SetInput([]byte{0x01})
	if err := key.SignTransaction(context.Background(), tx); err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return "0x" + hex.EncodeToString(raw)
}

func callHandler(t *testing.T, c evm.Client, keyless bool, in sendRawTxInput) (sendRawTxOutput, error) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := makeSendRawTxHandler(c, anchorHex, keyless, logger)
	_, out, err := h(context.Background(), &sdkmcp.CallToolRequest{}, in)
	return out, err
}

func TestSendRawTx_KeylessScope(t *testing.T) {
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	other := defitypes.MustAddressFromHex("0x00000000000000000000000000000000000000Ee")

	t.Run("keyless: anchor tx broadcasts canonical", func(t *testing.T) {
		raw := signedTxTo(t, key, anchor)
		cc := &captureClient{}
		out, err := callHandler(t, cc, true, sendRawTxInput{SignedTxHex: raw})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !cc.called {
			t.Fatal("SendRawTransaction not called")
		}
		if cc.gotHex != raw {
			t.Errorf("broadcast hex = %s, want canonical %s", cc.gotHex, raw)
		}
		if out.TxHash == "" {
			t.Error("empty tx hash")
		}
	})

	t.Run("keyless: non-anchor tx rejected, no broadcast", func(t *testing.T) {
		raw := signedTxTo(t, key, other)
		cc := &captureClient{}
		_, err := callHandler(t, cc, true, sendRawTxInput{SignedTxHex: raw})
		if !errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Errorf("err = %v, want ErrRelayScopeRejected", err)
		}
		if cc.called {
			t.Error("SendRawTransaction was called despite scope rejection")
		}
	})

	t.Run("authed (keyless off): raw hex passed through unchanged", func(t *testing.T) {
		raw := signedTxTo(t, key, other) // non-anchor allowed when scope is off
		cc := &captureClient{}
		_, err := callHandler(t, cc, false, sendRawTxInput{SignedTxHex: raw})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !cc.called || cc.gotHex != raw {
			t.Errorf("expected raw passthrough; called=%v hex=%s", cc.called, cc.gotHex)
		}
	})
}
