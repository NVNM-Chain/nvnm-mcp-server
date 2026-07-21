// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"io"
	"log/slog"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSendRawTx_RecordsWriteAuditInAuthedMode covers F1: an authed /
// self-host broadcast (keylessWrites=false) must still persist a
// signer-keyed audit row when a write-audit store is configured. Before
// the fix, the handler only decoded (and therefore only audited) under
// keyless writes, so the more-trusted authed path had the weaker trail.
func TestSendRawTx_RecordsWriteAuditInAuthedMode(t *testing.T) {
	fa := &fakeWriteAudit{}
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxTo(t, key, anchor)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// keylessWrites = false (authed mode), audit store configured.
	h := makeSendRawTxHandler(&captureClient{txHash: "0xdef"}, anchorHex, false, false, fa, nil, signerGates{}, logger)
	_, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(fa.recorded) != 1 {
		t.Fatalf("F1: authed broadcast must persist exactly 1 audit row, got %d", len(fa.recorded))
	}
	got := fa.recorded[0]
	if got.Outcome != "broadcast_ok" {
		t.Errorf("outcome = %q, want broadcast_ok", got.Outcome)
	}
	if got.TxHash != "0xdef" {
		t.Errorf("tx_hash = %q, want 0xdef", got.TxHash)
	}
	if got.Signer != key.Address().String() {
		t.Errorf("signer = %q, want %q (recovered from the signed tx)", got.Signer, key.Address().String())
	}
}
