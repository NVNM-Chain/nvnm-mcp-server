// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// buildSignedTxHex creates a valid signed transaction hex for testing.
func buildSignedTxHex(t *testing.T) string {
	t.Helper()
	key := defiwallet.NewRandomKey()
	return signTxWithKey(t, key)
}

func signTxWithKey(t *testing.T, key *defiwallet.PrivateKey) string {
	t.Helper()
	to := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000A00")
	tx := defitypes.NewTransaction().
		SetType(defitypes.LegacyTxType).
		SetNonce(0).
		SetGasPrice(big.NewInt(1000000000)).
		SetGasLimit(21000).
		SetTo(to).
		SetValue(big.NewInt(0)).
		SetInput([]byte{0xca, 0xfe}).
		SetChainID(58887)
	if err := key.SignTransaction(context.Background(), tx); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	raw, err := tx.Raw()
	if err != nil {
		t.Fatalf("marshal tx: %v", err)
	}
	return "0x" + hex.EncodeToString(raw)
}

type e2eServerConfig struct {
	evmClient *mockEVM
}

func startTestServerWithConfig(
	t *testing.T,
	cfg e2eServerConfig,
) *mcp.ClientSession {
	t.Helper()

	if cfg.evmClient == nil {
		cfg.evmClient = &mockEVM{
			chainInfo:  &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100},
			balance:    &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"},
			block:      &evm.NormalizedBlock{Number: 1, Hash: "0xabc"},
			sendTxHash: "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		}
	}
	anchorClient := &mockAnchor{
		info: anchor.PrecompileInfo{
			Address:     testAddr,
			ChainID:     58887,
			ABILoaded:   true,
			MethodCount: 5,
		},
	}

	srv := NewServer(cfg.evmClient, anchorClient, testServerConfig(true), nil, nil, testLogger())

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	httpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(httpServer.Close)

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "1.0.0"},
		nil,
	)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
	}, nil)
	if err != nil {
		t.Fatalf("connect to test server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// ---------------------------------------------------------------------------
// Write approval E2E tests
// ---------------------------------------------------------------------------

// TestE2E_SendRawTx_DirectBroadcast_NoElicitation asserts that
// evm_send_raw_transaction broadcasts directly with no elicitation round-trip,
// even with no client-side elicitation handler wired. Under Option 0 the
// server makes no server→client requests, so any approval default should
// result in a tx_hash response rather than an error.
func TestE2E_SendRawTx_DirectBroadcast_NoElicitation(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	// No ElicitationHandler wired — if the server were still calling
	// session.Elicit, the SDK would return an error and the tool call
	// would fail. With elicitation removed, it must succeed.
	session := startTestServerWithConfig(t, e2eServerConfig{})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected direct broadcast to succeed with no elicitation, got error: %v", result.Content)
	}
}

// TestE2E_SendRawTx_RPCError_BroadcastFails verifies that when the EVM RPC
// returns an error during broadcast the tool response has IsError=true.
func TestE2E_SendRawTx_RPCError_BroadcastFails(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		evmClient: &mockEVM{
			chainInfo:  &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100},
			balance:    &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"},
			block:      &evm.NormalizedBlock{Number: 1, Hash: "0xabc"},
			returnErr:  errors.New("nonce too low"),
			sendTxHash: "",
		},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for RPC failure")
	}
}

// TestSafeForClient_PassesThroughAuthRequired locks the contract that an
// anonymous caller hitting an auth-gated tool sees the "authentication
// required" sentinel at the client boundary, not the generic upstream-fail
// sentinel. Without this passthrough, the enforcement-middleware rejection
// would be sanitized into a misleading "service temporarily unavailable"
// reply.
func TestSafeForClient_PassesThroughAuthRequired(t *testing.T) {
	wrapped := fmt.Errorf("%w: tool %q", apperrors.ErrAuthRequired, "evm_send_raw_transaction")
	safe := apperrors.SafeForClient(wrapped)
	if !errors.Is(safe, apperrors.ErrAuthRequired) {
		t.Errorf("SafeForClient(%v) = %v, want ErrAuthRequired in chain", wrapped, safe)
	}
}

// ---------------------------------------------------------------------------
// API key authentication E2E tests
// ---------------------------------------------------------------------------

// testKeyLookup adapts a slice of KeyEntry to auth.KeyLookup for
// tests. Fixtures are expected to be built via NewKeyEntry so each
// entry's KeyHash is populated; legacy entries with only the raw Key
// field are also tolerated and hashed on the fly so the migration
// regression tests can exercise both shapes.
type testKeyLookup struct {
	entries []KeyEntry
}

func (t *testKeyLookup) Lookup(_ context.Context, rawKey string) (*auth.KeyResult, auth.RejectReason) {
	wantHash := auth.HashKey(rawKey)
	for i := range t.entries {
		if !t.entries[i].Enabled {
			continue
		}
		entryHash := t.entries[i].KeyHash
		if entryHash == "" && t.entries[i].Key != "" {
			entryHash = auth.HashKey(t.entries[i].Key)
		}
		if entryHash == wantHash {
			return &auth.KeyResult{
				ID:      t.entries[i].ID,
				KeyHash: entryHash,
				Roles:   t.entries[i].Roles,
			}, auth.RejectNone
		}
	}
	return nil, auth.RejectNotFound
}

func (t *testKeyLookup) Empty() bool {
	for i := range t.entries {
		if t.entries[i].Enabled {
			return false
		}
	}
	return true
}

// bearerTransport injects an Authorization header into every outgoing request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (bt *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if bt.token != "" {
		r.Header.Set("Authorization", "Bearer "+bt.token)
	}
	base := bt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

type authE2EConfig struct {
	keys []KeyEntry
	// keylessReads, when true, configures the test server with
	// cfg.KeylessReads=true and cfg.Transport="http" so NewServer
	// registers the enforcement middleware, AND passes keylessReads=true
	// to AuthMiddleware. Default false preserves existing test behavior.
	keylessReads bool
}

func startAuthTestServer(
	t *testing.T,
	cfg authE2EConfig,
	clientToken string,
) (*mcp.ClientSession, error) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	evmClient := &mockEVM{
		chainInfo:  &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100},
		balance:    &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"},
		block:      &evm.NormalizedBlock{Number: 1, Hash: "0xabc"},
		sendTxHash: "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	}
	anchorClient := &mockAnchor{
		info: anchor.PrecompileInfo{
			Address:     testAddr,
			ChainID:     58887,
			ABILoaded:   true,
			MethodCount: 5,
		},
	}

	var validator auth.TokenValidator
	if len(cfg.keys) > 0 {
		adapter := &testKeyLookup{entries: cfg.keys}
		validator = auth.NewAPIKeyValidator(adapter)
	}

	serverCfg := testServerConfig(true)
	if cfg.keylessReads {
		serverCfg.KeylessReads = true
		serverCfg.Transport = "http"
	}
	srv := NewServer(evmClient, anchorClient, serverCfg, nil, nil, logger)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	handler := AuthMiddleware(mcpHandler, validator, nil, cfg.keylessReads, logger, "")

	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	httpClient := &http.Client{
		Transport: &bearerTransport{token: clientToken},
	}

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-auth-client", Version: "1.0.0"},
		nil,
	)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   httpServer.URL,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = session.Close() })

	return session, nil
}

// TestE2E_StatelessHandler_ServesUnknownSession verifies the Option 0
// affinity-removing property at the HTTP layer: a request bearing a
// session id the handler has never seen -- exactly what happens when a
// load balancer routes a follow-up call to a different replica -- is
// rejected with 404 "session not found" by the stateful handler but
// served by the stateless one (go-sdk streamable.go session-id check).
// We exercise BOTH modes so the test proves it discriminates, rather
// than passing vacuously.
func TestE2E_StatelessHandler_ServesUnknownSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	evmClient := &mockEVM{
		chainInfo: &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100},
		balance:   &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"},
		block:     &evm.NormalizedBlock{Number: 1, Hash: "0xabc"},
	}
	anchorClient := &mockAnchor{info: anchor.PrecompileInfo{
		Address: testAddr, ChainID: 58887, ABILoaded: true, MethodCount: 5,
	}}
	srv := NewServer(evmClient, anchorClient, testServerConfig(false), nil, nil, logger)

	handler := func(stateless bool) http.Handler {
		return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return srv.mcpServer
		}, &mcp.StreamableHTTPOptions{Stateless: stateless})
	}

	// A follow-up POST carrying a session id the replica has never minted.
	postUnknownSession := func(h http.Handler) int {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Session-Id", "session-minted-on-another-replica")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Stateful: the unknown session id is rejected -> this is the failure a
	// multi-replica deployment hits without affinity. (Sanity: proves the
	// request shape actually reaches the session-id check.)
	if code := postUnknownSession(handler(false)); code != http.StatusNotFound {
		t.Fatalf("stateful handler: unknown-session status = %d, want 404 (session not found)", code)
	}
	// Stateless: the same request must NOT be rejected as session-not-found,
	// so any replica can serve any request -- no affinity required.
	if code := postUnknownSession(handler(true)); code == http.StatusNotFound {
		t.Fatal("stateless handler returned 404 for an unknown session id; affinity not removed")
	}
}

func TestE2E_Auth_ValidKey_ToolCallSucceeds(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", []string{"reader"})}
	session, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "valid-key-123")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
}

func TestE2E_Auth_InvalidKey_ConnectionFails(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", nil)}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "wrong-key-456")
	if err == nil {
		t.Fatal("expected connection to fail with invalid key")
	}
}

func TestE2E_Auth_MissingKey_ConnectionFails(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", nil)}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "")
	if err == nil {
		t.Fatal("expected connection to fail with missing key")
	}
}

func TestE2E_Auth_DisabledKey_ConnectionFails(t *testing.T) {
	disabled := NewKeyEntry("disabled-client", "disabled-key", nil)
	disabled.Enabled = false
	keys := []KeyEntry{
		NewKeyEntry("active-client", "active-key", nil),
		disabled,
	}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "disabled-key")
	if err == nil {
		t.Fatal("expected connection to fail with disabled key")
	}
}

func TestE2E_RBAC_ReaderCannotCallWriteTool(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("reader-only", "reader-key-123", []string{"reader"})}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys: keys,
	}, "reader-key-123")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Read tool should work
	readResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool read: %v", err)
	}
	if readResult.IsError {
		t.Fatalf("reader should be able to call read tools, got error: %v", readResult.Content)
	}

	// Write tool should be denied
	writeResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "anchor_prepare_add_record",
		Arguments: map[string]any{
			"from":     "0x1234567890abcdef1234567890abcdef12345678",
			"registry": "test",
			"checksum": "abc",
		},
	})
	if err != nil {
		t.Fatalf("CallTool write: %v", err)
	}
	if !writeResult.IsError {
		t.Fatal("reader should be denied from write tools")
	}
}

func TestE2E_RBAC_NoRolesDeniedAll(t *testing.T) {
	// Default-deny: an authenticated key with no roles authorizes nothing,
	// even an auth-exempt read tool -- the key is authenticated, so RBAC
	// applies (unlike a truly anonymous keyless-read request).
	keys := []KeyEntry{NewKeyEntry("no-role-client", "no-role-key", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys: keys,
	}, "no-role-key")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	readResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool read: %v", err)
	}
	if !readResult.IsError {
		t.Fatal("no-role key should be denied under default-deny")
	}
}

func TestE2E_RBAC_GrantRoleRequiresAdmin(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("writer-client", "writer-key", []string{"writer"})}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys: keys,
	}, "writer-key")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "anchor_prepare_grant_role",
		Arguments: map[string]any{
			"from":        "0x1234567890abcdef1234567890abcdef12345678",
			"registry_id": float64(1),
			"account":     "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			"role":        "editor",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("writer should be denied from grant_role (admin only)")
	}
}

func TestE2E_Auth_NoKeysConfigured_NoAuthRequired(t *testing.T) {
	session, err := startAuthTestServer(t, authE2EConfig{}, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
}

// TestE2E_Auth_ValidKey_SendTx_Succeeds verifies that an authenticated key
// broadcasts successfully under Option 0 (no elicitation, direct broadcast).
func TestE2E_Auth_ValidKey_SendTx_Succeeds(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	keys := []KeyEntry{NewKeyEntry("write-client", "write-key-789", []string{"writer"})}

	session, err := startAuthTestServer(t, authE2EConfig{
		keys: keys,
	}, "write-key-789")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
}

// ---------------------------------------------------------------------------
// Keyless-read E2E tests (Phase 9.16)
//
// These exercise the full keyless stack end-to-end: AuthMiddleware admits
// anonymous requests when the Authorization header is absent; the
// enforcement middleware (registered in NewServer for HTTP+keyless) rejects
// anonymous calls to auth-gated tools; SafeForClient preserves the
// ErrAuthRequired identity through the telemetry/error chain so the client
// sees a meaningful rejection.
// ---------------------------------------------------------------------------

// resultText concatenates the text content from an MCP CallToolResult
// into a single string for substring assertions. Works for both
// success and error results (the SDK puts both kinds of content in
// the same Content slice).
func resultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestE2E_Keyless_AnonReadAllowed(t *testing.T) {
	// Non-obvious: even though we're testing the anonymous code path,
	// startAuthTestServer only constructs an auth.TokenValidator when
	// cfg.keys is non-empty. Without a validator, AuthMiddleware
	// short-circuits and the keyless behavior is not exercised. So we
	// configure a key but the client sends no Authorization header
	// (clientToken == "").
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:         keys,
		keylessReads: true,
	}, "") // empty token -> bearerTransport omits the Authorization header
	if err != nil {
		t.Fatalf("anon connect under keyless: %v", err)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("anon read tool must succeed under keyless; got error: %v", result.Content)
	}
}

func TestE2E_Keyless_AnonWriteRejected(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:         keys,
		keylessReads: true,
	}, "")
	if err != nil {
		t.Fatalf("anon connect under keyless: %v", err)
	}

	signedTx := buildSignedTxHex(t)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	// The enforcement middleware returns (nil, error) from the method
	// handler. The SDK surfaces this as a call-level err (not as a tool
	// result with IsError=true, which is the shape RBAC rejection uses
	// because RBAC runs INSIDE the tool handler). Accept either form so
	// the test stays correct if the SDK changes its surfacing.
	var msg string
	switch {
	case err != nil:
		msg = err.Error()
	case result != nil && result.IsError:
		msg = resultText(result)
	default:
		t.Fatalf("anon write tool must be rejected; got success: %v", result.Content)
	}
	if !strings.Contains(msg, apperrors.ErrAuthRequired.Error()) {
		t.Errorf("rejection must mention 'authentication required'; got: %s", msg)
	}
}

func TestE2E_Keyless_AuthedWriteReachesHandler(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:         keys,
		keylessReads: true,
	}, "valid-key-123")
	if err != nil {
		t.Fatalf("authed connect under keyless: %v", err)
	}

	signedTx := buildSignedTxHex(t)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	// Domain success or non-auth domain error are both acceptable; an
	// auth-required surface (in either err or result content) is the
	// regression we guard against. Check both shapes.
	var msg string
	switch {
	case err != nil:
		msg = err.Error()
	case result != nil && result.IsError:
		msg = resultText(result)
	}
	if strings.Contains(msg, apperrors.ErrAuthRequired.Error()) {
		t.Fatalf("authed write must not see auth-required rejection; got: %s", msg)
	}
}
