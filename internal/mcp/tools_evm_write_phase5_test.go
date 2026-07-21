// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBlacklist is an in-memory SignerBlacklistStore for unit tests.
type fakeBlacklist struct {
	banned map[string]bool
	err    error
}

func (f *fakeBlacklist) IsBlacklisted(_ context.Context, s string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.banned[strings.ToLower(s)], nil
}

func (f *fakeBlacklist) Add(context.Context, string, string) error { return nil }
func (f *fakeBlacklist) Remove(context.Context, string) error      { return nil }
func (f *fakeBlacklist) List(context.Context) ([]BlacklistEntry, error) {
	return nil, nil
}

// fakeQuota is an in-memory SignerQuotaStore for unit tests.
type fakeQuota struct {
	counts map[string]int
	err    error
}

func (f *fakeQuota) Count(_ context.Context, s string, _ time.Time) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[strings.ToLower(s)], nil
}

func (f *fakeQuota) Increment(_ context.Context, s string, _ time.Time) error {
	if f.err != nil {
		return f.err
	}
	if f.counts == nil {
		f.counts = map[string]int{}
	}
	f.counts[strings.ToLower(s)]++
	return nil
}

// TestSendRawTx_Phase5_Enforcement exercises the per-signer blacklist +
// quota gates inserted into the keyless write path: blacklist is consulted
// before quota, both fail closed by default (opt-in fail-open), and the
// quota counter is only incremented after a successful broadcast.
func TestSendRawTx_Phase5_Enforcement(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	anchor := defitypes.MustAddressFromHex(anchorHex)

	newSignedTx := func(t *testing.T) (string, string) {
		t.Helper()
		key := wallet.NewRandomKey()
		raw := signedTxTo(t, key, anchor)
		return raw, key.Address().String()
	}

	t.Run("blacklisted signer rejected, quota never consulted", func(t *testing.T) {
		raw, signer := newSignedTx(t)
		bl := &fakeBlacklist{banned: map[string]bool{strings.ToLower(signer): true}}
		q := &fakeQuota{counts: map[string]int{}}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{blacklist: bl, quota: q, rate: 10, window: time.Hour}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if !errors.Is(err, ErrSignerBlacklisted) {
			t.Fatalf("err = %v, want ErrSignerBlacklisted", err)
		}
		if cc.called {
			t.Error("broadcast must not be called for a blacklisted signer")
		}
		if got := q.counts[strings.ToLower(signer)]; got != 0 {
			t.Errorf("quota count = %d, want 0 (blacklist short-circuits before quota)", got)
		}
	})

	t.Run("at-quota signer rejected", func(t *testing.T) {
		raw, signer := newSignedTx(t)
		q := &fakeQuota{counts: map[string]int{strings.ToLower(signer): 5}}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{quota: q, rate: 5, window: time.Hour}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if !errors.Is(err, ErrSignerQuotaExceeded) {
			t.Fatalf("err = %v, want ErrSignerQuotaExceeded", err)
		}
		if cc.called {
			t.Error("broadcast must not be called once the signer is at quota")
		}
	})

	t.Run("blacklist store error, fail-closed rejects", func(t *testing.T) {
		raw, _ := newSignedTx(t)
		bl := &fakeBlacklist{err: errors.New("db down")}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{blacklist: bl, blacklistFailOpen: false}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if err == nil {
			t.Fatal("expected error when the blacklist store errors and fail-closed is in effect")
		}
		if cc.called {
			t.Error("broadcast must not be called on a fail-closed blacklist store error")
		}
	})

	t.Run("blacklist store error, fail-open allows broadcast", func(t *testing.T) {
		raw, _ := newSignedTx(t)
		bl := &fakeBlacklist{err: errors.New("db down")}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{blacklist: bl, blacklistFailOpen: true}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if err != nil {
			t.Fatalf("err = %v, want nil (fail-open)", err)
		}
		if !cc.called {
			t.Error("broadcast must be called on a fail-open blacklist store error")
		}
	})

	t.Run("quota store error, fail-closed rejects", func(t *testing.T) {
		raw, _ := newSignedTx(t)
		q := &fakeQuota{err: errors.New("db down")}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{quota: q, rate: 5, window: time.Hour, quotaFailOpen: false}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if err == nil {
			t.Fatal("expected error when the quota store errors and fail-closed is in effect")
		}
		if cc.called {
			t.Error("broadcast must not be called on a fail-closed quota store error")
		}
	})

	t.Run("quota store error, fail-open allows broadcast", func(t *testing.T) {
		raw, _ := newSignedTx(t)
		q := &fakeQuota{err: errors.New("db down")}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{quota: q, rate: 5, window: time.Hour, quotaFailOpen: true}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if err != nil {
			t.Fatalf("err = %v, want nil (fail-open)", err)
		}
		if !cc.called {
			t.Error("broadcast must be called on a fail-open quota store error")
		}
	})

	t.Run("happy path: broadcast succeeds and quota is incremented once", func(t *testing.T) {
		raw, signer := newSignedTx(t)
		q := &fakeQuota{counts: map[string]int{}}
		cc := &captureClient{txHash: "0xabc"}
		gates := signerGates{quota: q, rate: 10, window: time.Hour}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !cc.called {
			t.Fatal("broadcast was not called")
		}
		if got := q.counts[strings.ToLower(signer)]; got != 1 {
			t.Errorf("quota count = %d, want 1", got)
		}
	})

	t.Run("broadcast failure does not increment quota", func(t *testing.T) {
		raw, signer := newSignedTx(t)
		q := &fakeQuota{counts: map[string]int{}}
		cc := &captureClient{err: errors.New("rpc down")}
		gates := signerGates{quota: q, rate: 10, window: time.Hour}
		h := makeSendRawTxHandler(cc, anchorHex, true, false, nil, nil, gates, logger)

		_, _, err := h(ctx, &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if err == nil {
			t.Fatal("expected broadcast error to propagate")
		}
		if got := q.counts[strings.ToLower(signer)]; got != 0 {
			t.Errorf("quota count = %d, want 0 (broadcast failed, must not increment)", got)
		}
	})
}
