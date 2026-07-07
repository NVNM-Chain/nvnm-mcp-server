// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

const anchorHex = "0x0000000000000000000000000000000000000A00"

// captureClient implements evm.Client by embedding the interface (nil) and
// overriding only SendRawTransaction, the sole method the handler calls.
type captureClient struct {
	evm.Client
	gotHex string
	called bool
	txHash string
	err    error
}

func (c *captureClient) SendRawTransaction(_ context.Context, signedTxHex string) (string, error) {
	c.called = true
	c.gotHex = signedTxHex
	if c.err != nil {
		return "", c.err
	}
	if c.txHash == "" {
		c.txHash = "0xhash"
	}
	return c.txHash, nil
}

// fakeWriteMetrics is an in-memory WriteMetrics recorder for unit tests.
type fakeWriteMetrics struct {
	broadcasts []string
	rejects    []string
}

func (f *fakeWriteMetrics) RecordBroadcast(_ context.Context, outcome string) {
	f.broadcasts = append(f.broadcasts, outcome)
}

func (f *fakeWriteMetrics) RecordRelayReject(_ context.Context, cause telemetry.RelayRejectCause) {
	f.rejects = append(f.rejects, string(cause))
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
	h := makeSendRawTxHandler(c, anchorHex, keyless, nil, nil, signerGates{}, logger)
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

// fakeWriteAudit is an in-memory WriteAuditStore for unit tests. Task 4 reuses
// it from the same package.
type fakeWriteAudit struct{ recorded []WriteAuditEntry }

//nolint:gocritic // hugeParam: WriteAuditEntry matches the interface signature; pointer would diverge.
func (f *fakeWriteAudit) Record(_ context.Context, e WriteAuditEntry) error {
	f.recorded = append(f.recorded, e)
	return nil
}

func (f *fakeWriteAudit) Query(_ context.Context, _ WriteAuditFilter) ([]WriteAuditEntry, error) {
	return f.recorded, nil
}

func TestSendRawTx_RecordsWriteAuditOnSuccess(t *testing.T) {
	fa := &fakeWriteAudit{}
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxTo(t, key, anchor)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := makeSendRawTxHandler(&captureClient{txHash: "0xabc"}, anchorHex, true, fa, nil, signerGates{}, logger)
	_, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(fa.recorded) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(fa.recorded))
	}
	got := fa.recorded[0]
	if got.Outcome != "broadcast_ok" {
		t.Errorf("outcome = %q, want broadcast_ok", got.Outcome)
	}
	if got.TxHash != "0xabc" {
		t.Errorf("tx_hash = %q, want 0xabc", got.TxHash)
	}
	if got.Signer == "" {
		t.Error("signer is empty")
	}
	if got.Signer != key.Address().String() {
		t.Errorf("signer = %q, want %q", got.Signer, key.Address().String())
	}
}

func TestSendRawTx_RecordsWriteAuditOnFailure(t *testing.T) {
	fa := &fakeWriteAudit{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxTo(t, key, anchor)

	boom := errors.New("rpc down")
	h := makeSendRawTxHandler(&captureClient{err: boom}, anchorHex, true, fa, nil, signerGates{}, logger)
	_, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
	if err == nil {
		t.Fatal("expected broadcast error to propagate")
	}
	if len(fa.recorded) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(fa.recorded))
	}
	got := fa.recorded[0]
	if got.Outcome != "broadcast_failed" {
		t.Errorf("outcome = %q, want broadcast_failed", got.Outcome)
	}
	if got.Error == "" {
		t.Error("expected non-empty Error on failure row")
	}
	if got.Signer != key.Address().String() {
		t.Errorf("signer = %q, want %q", got.Signer, key.Address().String())
	}
}

func TestSendRawTx_NilWriteAuditNoPanic(t *testing.T) {
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxTo(t, key, anchor)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := makeSendRawTxHandler(&captureClient{txHash: "0xabc"}, anchorHex, true, nil, nil, signerGates{}, logger)
	if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err != nil {
		t.Fatalf("handler with nil store: %v", err)
	}
}

func TestSendRawTx_SignerAudit(t *testing.T) {
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxTo(t, key, anchor)

	// keyless: success audit log is signer-keyed (signer/to/value/calldata),
	// not client_id.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := makeSendRawTxHandler(&captureClient{txHash: "0xabc"}, anchorHex, true, nil, nil, signerGates{}, logger)
	if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err != nil {
		t.Fatalf("keyless broadcast err: %v", err)
	}
	logStr := buf.String()
	for _, want := range []string{
		`"phase":"broadcast_ok"`, `"signer"`, key.Address().String(),
		`"to"`, `"value_wei"`, `"calldata_len"`, "0xabc",
	} {
		if !strings.Contains(logStr, want) {
			t.Errorf("keyless audit log missing %q\nlog: %s", want, logStr)
		}
	}

	// authed (keyless off): success audit log keeps client_id, no signer field.
	buf.Reset()
	h2 := makeSendRawTxHandler(&captureClient{txHash: "0xdef"}, anchorHex, false, nil, nil, signerGates{}, logger)
	if _, _, err := h2(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err != nil {
		t.Fatalf("authed broadcast err: %v", err)
	}
	authed := buf.String()
	if strings.Contains(authed, `"signer"`) {
		t.Errorf("authed audit log should not contain signer field\nlog: %s", authed)
	}
	if !strings.Contains(authed, `"client_id"`) {
		t.Errorf("authed audit log should contain client_id\nlog: %s", authed)
	}
}

func TestSendRawTx_Metrics(t *testing.T) {
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex(anchorHex)
	other := defitypes.MustAddressFromHex("0x00000000000000000000000000000000000000Ee")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("broadcast ok records outcome=ok", func(t *testing.T) {
		fm := &fakeWriteMetrics{}
		raw := signedTxTo(t, key, anchor)
		h := makeSendRawTxHandler(&captureClient{txHash: "0xabc"}, anchorHex, true, nil, fm, signerGates{}, logger)
		if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err != nil {
			t.Fatalf("err = %v", err)
		}
		if !slices.Equal(fm.broadcasts, []string{"ok"}) {
			t.Errorf("broadcasts = %v, want [ok]", fm.broadcasts)
		}
		if len(fm.rejects) != 0 {
			t.Errorf("rejects = %v, want none", fm.rejects)
		}
	})

	t.Run("broadcast failure records outcome=failed", func(t *testing.T) {
		fm := &fakeWriteMetrics{}
		raw := signedTxTo(t, key, anchor)
		h := makeSendRawTxHandler(&captureClient{err: errors.New("boom")}, anchorHex, true, nil, fm, signerGates{}, logger)
		if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err == nil {
			t.Fatal("expected broadcast error")
		}
		if !slices.Equal(fm.broadcasts, []string{"failed"}) {
			t.Errorf("broadcasts = %v, want [failed]", fm.broadcasts)
		}
	})

	t.Run("relay-scope reject records cause=relay_scope", func(t *testing.T) {
		fm := &fakeWriteMetrics{}
		raw := signedTxTo(t, key, other)
		h := makeSendRawTxHandler(&captureClient{}, anchorHex, true, nil, fm, signerGates{}, logger)
		if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err == nil {
			t.Fatal("expected relay-scope rejection")
		}
		if !slices.Equal(fm.rejects, []string{"relay_scope"}) {
			t.Errorf("rejects = %v, want [relay_scope]", fm.rejects)
		}
		if len(fm.broadcasts) != 0 {
			t.Errorf("broadcasts = %v, want none", fm.broadcasts)
		}
	})

	t.Run("decode failure records cause=decode", func(t *testing.T) {
		fm := &fakeWriteMetrics{}
		h := makeSendRawTxHandler(&captureClient{}, anchorHex, true, nil, fm, signerGates{}, logger)
		if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: "0xzzzz"}); err == nil {
			t.Fatal("expected decode error")
		}
		if !slices.Equal(fm.rejects, []string{"decode"}) {
			t.Errorf("rejects = %v, want [decode]", fm.rejects)
		}
	})

	t.Run("anchor misconfig records cause=anchor_misconfig", func(t *testing.T) {
		fm := &fakeWriteMetrics{}
		raw := signedTxTo(t, key, anchor)
		h := makeSendRawTxHandler(&captureClient{}, "not-an-address", true, nil, fm, signerGates{}, logger)
		if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err == nil {
			t.Fatal("expected anchor misconfig error")
		}
		if !slices.Equal(fm.rejects, []string{"anchor_misconfig"}) {
			t.Errorf("rejects = %v, want [anchor_misconfig]", fm.rejects)
		}
	})

	t.Run("nil metrics does not panic", func(t *testing.T) {
		raw := signedTxTo(t, key, anchor)
		h := makeSendRawTxHandler(&captureClient{txHash: "0xabc"}, anchorHex, true, nil, nil, signerGates{}, logger)
		if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}
