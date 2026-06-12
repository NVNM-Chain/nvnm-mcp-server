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
	approvalDefault string
	evmClient       *mockEVM
}

func startTestServerWithConfig(
	t *testing.T,
	cfg e2eServerConfig,
	clientOpts *mcp.ClientOptions,
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

	approval := cfg.approvalDefault
	if approval == "" {
		approval = ApprovalRequired
	}

	srv := NewServer(cfg.evmClient, anchorClient, testServerConfig(true, approval), nil, testLogger())

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	httpServer := httptest.NewServer(mcpHandler)
	t.Cleanup(httpServer.Close)

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "1.0.0"},
		clientOpts,
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

func TestE2E_SendRawTx_AutoApproval_Succeeds(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalAuto,
	}, nil)

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

func TestE2E_SendRawTx_RequiredApproval_Accepted(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalRequired,
	}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success after human accept, got error: %v", result.Content)
	}
}

func TestE2E_SendRawTx_RequiredApproval_Declined(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalRequired,
	}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "decline"}, nil
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
		t.Fatal("expected IsError=true when user declines")
	}
	if len(result.Content) > 0 {
		tc, ok := result.Content[0].(*mcp.TextContent)
		if ok && !strings.Contains(tc.Text, "declined") {
			t.Errorf("error message should mention declined, got: %s", tc.Text)
		}
	}
}

func TestE2E_SendRawTx_RequiredApproval_Canceled(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalRequired,
	}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "cancel"}, nil
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
		t.Fatal("expected IsError=true when user cancels")
	}
}

func TestE2E_SendRawTx_NoElicitation_RequiredFails(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalRequired,
	}, nil)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when client lacks elicitation support")
	}
	if len(result.Content) > 0 {
		tc, ok := result.Content[0].(*mcp.TextContent)
		if ok && !strings.Contains(tc.Text, "elicitation") {
			t.Errorf("error should mention elicitation, got: %s", tc.Text)
		}
	}
}

func TestE2E_SendRawTx_ElicitationPromptContainsTxDetails(t *testing.T) {
	signedTx := buildSignedTxHex(t)
	var capturedMessage string

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalRequired,
	}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			capturedMessage = req.Params.Message
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})

	_, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	for _, expected := range []string{"To:", "Value:", "Gas:", "Nonce:", "Chain ID:", "Data:", "irreversible"} {
		if !strings.Contains(capturedMessage, expected) {
			t.Errorf("approval prompt missing %q, got:\n%s", expected, capturedMessage)
		}
	}
}

// TestE2E_SendRawTx_ElicitationRequestIsSpecCompliant asserts the write-approval
// elicitation carries a valid form `requestedSchema` and `mode: "form"`. Per the
// MCP spec a form elicitation MUST include a requestedSchema; a request with only
// a message (no schema) is rejected by spec-strict clients (e.g. Claude / Fable 5),
// so the broadcast can never complete from those clients. The go-sdk in-process
// test client is lenient (a nil schema validates), which is why this gap was
// invisible until a strict client hit it -- so the assertion must inspect the
// OUTGOING request params, not the mocked reply.
func TestE2E_SendRawTx_ElicitationRequestIsSpecCompliant(t *testing.T) {
	signedTx := buildSignedTxHex(t)
	var elicited bool
	var capturedSchema any
	var capturedMode string

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalRequired,
	}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			elicited = true
			capturedSchema = req.Params.RequestedSchema
			capturedMode = req.Params.Mode
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})

	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_send_raw_transaction",
		Arguments: map[string]any{"signed_tx": signedTx},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !elicited {
		t.Fatal("elicitation was not requested under required approval")
	}
	if capturedSchema == nil {
		t.Fatal("ElicitParams.RequestedSchema is nil; spec-strict clients reject a form elicitation with no schema")
	}
	schemaMap, ok := capturedSchema.(map[string]any)
	if !ok {
		t.Fatalf("RequestedSchema should marshal to an object map, got %T", capturedSchema)
	}
	if schemaMap["type"] != "object" {
		t.Errorf("RequestedSchema[\"type\"] = %v, want \"object\"", schemaMap["type"])
	}
	if _, hasProps := schemaMap["properties"]; !hasProps {
		t.Error("RequestedSchema should declare a \"properties\" object")
	}
	if capturedMode != "form" {
		t.Errorf("ElicitParams.Mode = %q, want \"form\"", capturedMode)
	}
}

func TestE2E_SendRawTx_AutoApproval_RPCError(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	session := startTestServerWithConfig(t, e2eServerConfig{
		approvalDefault: ApprovalAuto,
		evmClient: &mockEVM{
			chainInfo:  &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100},
			balance:    &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"},
			block:      &evm.NormalizedBlock{Number: 1, Hash: "0xabc"},
			returnErr:  errors.New("nonce too low"),
			sendTxHash: "",
		},
	}, nil)

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

// ---------------------------------------------------------------------------
// Approval + error sentinel tests (unit level, but depend on real tx parsing)
// ---------------------------------------------------------------------------

func TestDecodeTxSummary_ValidSignedTx(t *testing.T) {
	signedTx := buildSignedTxHex(t)
	summary, err := DecodeTxSummary(signedTx, "test-client", "testnet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.To != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("To = %q, want precompile address", summary.To)
	}
	if summary.Gas != 21000 {
		t.Errorf("Gas = %d, want 21000", summary.Gas)
	}
	if summary.DataLen != 2 {
		t.Errorf("DataLen = %d, want 2", summary.DataLen)
	}
	if summary.ClientID != "test-client" {
		t.Errorf("ClientID = %q, want %q", summary.ClientID, "test-client")
	}
}

func TestDecodeTxSummary_InvalidHex(t *testing.T) {
	_, err := DecodeTxSummary("0xZZZZ", "client", "testnet")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestFormatApprovalMessage_ContainsFields(t *testing.T) {
	summary := &TxSummary{
		To:       "0x0000000000000000000000000000000000000A00",
		Value:    big.NewInt(0),
		Gas:      65000,
		Nonce:    42,
		ChainID:  big.NewInt(58887),
		DataLen:  136,
		ClientID: "dev-agent",
	}
	msg := formatApprovalMessage(summary)
	for _, want := range []string{
		"0x0000000000000000000000000000000000000A00",
		"0 wei", "65000", "42", "58887", "136 bytes", "dev-agent", "irreversible",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

func TestErrWriteDeclined_IsSentinel(t *testing.T) {
	err := apperrors.ErrWriteDeclined
	if !errors.Is(err, apperrors.ErrWriteDeclined) {
		t.Error("ErrWriteDeclined should match via errors.Is")
	}
}

func TestErrElicitationUnsupported_IsSentinel(t *testing.T) {
	err := apperrors.ErrElicitationUnsupported
	if !errors.Is(err, apperrors.ErrElicitationUnsupported) {
		t.Error("ErrElicitationUnsupported should match via errors.Is")
	}
}

func TestSafeForClient_PassesThroughApprovalErrors(t *testing.T) {
	tests := []error{apperrors.ErrWriteDeclined, apperrors.ErrElicitationUnsupported}
	for _, sentinel := range tests {
		safe := apperrors.SafeForClient(sentinel)
		if !errors.Is(safe, sentinel) {
			t.Errorf("SafeForClient(%v) = %v, want original sentinel", sentinel, safe)
		}
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

func (t *testKeyLookup) Lookup(rawKey string) *auth.KeyResult {
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
				ID:            t.entries[i].ID,
				KeyHash:       entryHash,
				WriteApproval: t.entries[i].WriteApproval,
				Roles:         t.entries[i].Roles,
			}
		}
	}
	return nil
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
	keys            []KeyEntry
	approvalDefault string
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
	clientOpts *mcp.ClientOptions,
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

	approval := cfg.approvalDefault
	if approval == "" {
		approval = ApprovalRequired
	}

	var validator auth.TokenValidator
	if len(cfg.keys) > 0 {
		adapter := &testKeyLookup{entries: cfg.keys}
		validator = auth.NewAPIKeyValidator(adapter)
	}

	serverCfg := testServerConfig(true, approval)
	if cfg.keylessReads {
		serverCfg.KeylessReads = true
		serverCfg.Transport = "http"
	}
	srv := NewServer(evmClient, anchorClient, serverCfg, nil, logger)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	handler := AuthMiddleware(mcpHandler, validator, nil, cfg.keylessReads, logger)

	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	httpClient := &http.Client{
		Transport: &bearerTransport{token: clientToken},
	}

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-auth-client", Version: "1.0.0"},
		clientOpts,
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

func TestE2E_Auth_ValidKey_ToolCallSucceeds(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", "", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{keys: keys, approvalDefault: ApprovalAuto}, "valid-key-123", nil)
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
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", "", nil)}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "wrong-key-456", nil)
	if err == nil {
		t.Fatal("expected connection to fail with invalid key")
	}
}

func TestE2E_Auth_MissingKey_ConnectionFails(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", "", nil)}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "", nil)
	if err == nil {
		t.Fatal("expected connection to fail with missing key")
	}
}

func TestE2E_Auth_DisabledKey_ConnectionFails(t *testing.T) {
	disabled := NewKeyEntry("disabled-client", "disabled-key", "", nil)
	disabled.Enabled = false
	keys := []KeyEntry{
		NewKeyEntry("active-client", "active-key", "", nil),
		disabled,
	}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "disabled-key", nil)
	if err == nil {
		t.Fatal("expected connection to fail with disabled key")
	}
}

func TestE2E_RBAC_ReaderCannotCallWriteTool(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("reader-only", "reader-key-123", "", []string{"reader"})}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		approvalDefault: ApprovalAuto,
	}, "reader-key-123", nil)
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

func TestE2E_RBAC_NoRolesNoEnforcement(t *testing.T) {
	// API key with no roles should have no RBAC enforcement.
	keys := []KeyEntry{NewKeyEntry("no-role-client", "no-role-key", "", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		approvalDefault: ApprovalAuto,
	}, "no-role-key", nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Both read and write tools should work without role enforcement
	readResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool read: %v", err)
	}
	if readResult.IsError {
		t.Fatalf("no-role key should pass read tools: %v", readResult.Content)
	}
}

func TestE2E_RBAC_GrantRoleRequiresAdmin(t *testing.T) {
	keys := []KeyEntry{NewKeyEntry("writer-client", "writer-key", "", []string{"writer"})}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		approvalDefault: ApprovalAuto,
	}, "writer-key", nil)
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
	session, err := startAuthTestServer(t, authE2EConfig{approvalDefault: ApprovalAuto}, "", nil)
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

// ---------------------------------------------------------------------------
// Per-client write approval override E2E tests
// ---------------------------------------------------------------------------

func TestE2E_PerClientApproval_AutoOverridesGlobalRequired(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	keys := []KeyEntry{NewKeyEntry("auto-client", "auto-key-123", ApprovalAuto, nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		approvalDefault: ApprovalRequired,
	}, "auto-key-123", nil)
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
		t.Fatalf("expected auto-approval to succeed, got error: %v", result.Content)
	}
}

func TestE2E_PerClientApproval_RequiredOverridesGlobalAuto(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	keys := []KeyEntry{NewKeyEntry("strict-client", "strict-key-123", ApprovalRequired, nil)}

	elicitCalled := false
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		approvalDefault: ApprovalAuto,
	}, "strict-key-123", &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			elicitCalled = true
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})
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
		t.Fatalf("expected success after elicitation accept, got error: %v", result.Content)
	}
	if !elicitCalled {
		t.Error("expected elicitation handler to be called for per-client required override")
	}
}

func TestE2E_Auth_ValidKey_SendTx_WithElicitation(t *testing.T) {
	signedTx := buildSignedTxHex(t)

	keys := []KeyEntry{NewKeyEntry("write-client", "write-key-789", "", nil)}

	var capturedMessage string
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		approvalDefault: ApprovalRequired,
	}, "write-key-789", &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			capturedMessage = req.Params.Message
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})
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
	if !strings.Contains(capturedMessage, "Submitted by:       write-client") {
		t.Errorf("approval prompt should contain client identity, got:\n%s", capturedMessage)
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
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", "", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		keylessReads:    true,
		approvalDefault: ApprovalAuto,
	}, "", nil) // empty token -> bearerTransport omits the Authorization header
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
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", "", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		keylessReads:    true,
		approvalDefault: ApprovalAuto,
	}, "", nil)
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
	keys := []KeyEntry{NewKeyEntry("test-client", "valid-key-123", "", nil)}
	session, err := startAuthTestServer(t, authE2EConfig{
		keys:            keys,
		keylessReads:    true,
		approvalDefault: ApprovalAuto,
	}, "valid-key-123", nil)
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
