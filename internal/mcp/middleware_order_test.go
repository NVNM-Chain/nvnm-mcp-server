// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// TestMiddlewareOrder locks the outer security-guard order assembled by
// wrapSecurityChain: originGuard MUST run before (outside) the C5
// requireForwardedHTTPS check. This matters because a forged
// X-Forwarded-Proto header must never let a disallowed-Origin request
// reach any handler logic -- the Origin guard, the cheaper and more
// fundamental anti-spoof check, has to reject first regardless of what
// a downstream (and potentially proxy-spoofable) header claims.
//
// The three cases below send the SAME bad X-Forwarded-Proto value in
// two of them and vary only the Origin header:
//   - bad Origin + bad XFP  -> rejected by originGuard (outer)
//   - good Origin + bad XFP -> passes originGuard, rejected by C5 (inner)
//   - good Origin + good XFP -> passes both, reaches the sentinel handler
//
// Comparing case 1 against case 2 proves originGuard sits outside C5:
// only the request with a disallowed Origin is stopped before ever
// reaching the C5 check, even though both carry the same forged
// X-Forwarded-Proto. The two guards also emit distinguishable 403
// bodies ("origin not allowed" vs "https required"), so each case
// asserts on body text in addition to the sentinel-reached flag.
func TestMiddlewareOrder(t *testing.T) {
	const allowedOrigin = "https://claude.ai"

	evmClient := &mockEVM{
		chainInfo: &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100},
		balance:   &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"},
		block:     &evm.NormalizedBlock{Number: 1, Hash: "0xabc"},
	}
	anchorClient := &mockAnchor{
		info: anchor.PrecompileInfo{
			Address:     testAddr,
			ChainID:     58887,
			ABILoaded:   true,
			MethodCount: 5,
		},
	}
	srv := NewServer(evmClient, anchorClient, testServerConfig(true), nil, nil, nil, nil, nil, testLogger())

	allow := NewOriginAllowlist([]string{allowedOrigin})

	reached := false
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// trustProxyHeaders=true activates C5 (requireForwardedHTTPS); nil
	// failLimiter and nil metrics are passthroughs.
	handler := srv.wrapSecurityChain(sentinel, nil, allow, true, nil)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	tests := []struct {
		name       string
		origin     string
		forwarded  string
		wantStatus int
		wantBody   string
		wantReach  bool
	}{
		{
			name:       "bad origin + bad XFP rejected by originGuard before C5",
			origin:     "https://attacker.example.com",
			forwarded:  "http",
			wantStatus: http.StatusForbidden,
			wantBody:   "origin not allowed",
			wantReach:  false,
		},
		{
			name:       "good origin + bad XFP passes originGuard, rejected by C5",
			origin:     allowedOrigin,
			forwarded:  "http",
			wantStatus: http.StatusForbidden,
			wantBody:   "https required",
			wantReach:  false,
		},
		{
			name:       "good origin + good XFP reaches sentinel",
			origin:     allowedOrigin,
			forwarded:  "https",
			wantStatus: http.StatusOK,
			wantReach:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached = false

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, http.NoBody)
			if err != nil {
				t.Fatalf("NewRequestWithContext: %v", err)
			}
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-Proto", tc.forwarded)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if reached != tc.wantReach {
				t.Errorf("sentinel reached = %v, want %v", reached, tc.wantReach)
			}
			if tc.wantBody != "" {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if !strings.Contains(string(body), tc.wantBody) {
					t.Errorf("body = %q, want substring %q", string(body), tc.wantBody)
				}
			}
		})
	}
}
