package mcp

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
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

	srv := NewServer(cfg.evmClient, anchorClient, true, approval, "testnet", nil, testLogger())

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

// ---------------------------------------------------------------------------
// API key authentication E2E tests
// ---------------------------------------------------------------------------

// testKeyLookup adapts a slice of KeyEntry to auth.KeyLookup for tests.
type testKeyLookup struct {
	entries []KeyEntry
}

func (t *testKeyLookup) Lookup(rawKey string) *auth.KeyResult {
	for i := range t.entries {
		if t.entries[i].Enabled && t.entries[i].Key == rawKey {
			return &auth.KeyResult{
				ID:            t.entries[i].ID,
				Key:           t.entries[i].Key,
				WriteApproval: t.entries[i].WriteApproval,
				Roles:         t.entries[i].Roles,
			}
		}
	}
	return nil
}

func (t *testKeyLookup) Empty() bool {
	for _, e := range t.entries {
		if e.Enabled {
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

	srv := NewServer(evmClient, anchorClient, true, approval, "testnet", nil, logger)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	handler := AuthMiddleware(mcpHandler, validator, nil, logger)

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
	keys := []KeyEntry{
		{ID: "test-client", Key: "valid-key-123", Enabled: true, CreatedAt: time.Now()},
	}
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
	keys := []KeyEntry{
		{ID: "test-client", Key: "valid-key-123", Enabled: true, CreatedAt: time.Now()},
	}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "wrong-key-456", nil)
	if err == nil {
		t.Fatal("expected connection to fail with invalid key")
	}
}

func TestE2E_Auth_MissingKey_ConnectionFails(t *testing.T) {
	keys := []KeyEntry{
		{ID: "test-client", Key: "valid-key-123", Enabled: true, CreatedAt: time.Now()},
	}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "", nil)
	if err == nil {
		t.Fatal("expected connection to fail with missing key")
	}
}

func TestE2E_Auth_DisabledKey_ConnectionFails(t *testing.T) {
	keys := []KeyEntry{
		{ID: "active-client", Key: "active-key", Enabled: true, CreatedAt: time.Now()},
		{ID: "disabled-client", Key: "disabled-key", Enabled: false, CreatedAt: time.Now()},
	}
	_, err := startAuthTestServer(t, authE2EConfig{keys: keys}, "disabled-key", nil)
	if err == nil {
		t.Fatal("expected connection to fail with disabled key")
	}
}

func TestE2E_RBAC_ReaderCannotCallWriteTool(t *testing.T) {
	keys := []KeyEntry{
		{
			ID:      "reader-only",
			Key:     "reader-key-123",
			Enabled: true,
			Roles:   []string{"reader"},
		},
	}
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
	// API key with no roles should have no RBAC enforcement
	keys := []KeyEntry{
		{
			ID:      "no-role-client",
			Key:     "no-role-key",
			Enabled: true,
			// Roles intentionally omitted
		},
	}
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
	keys := []KeyEntry{
		{
			ID:      "writer-client",
			Key:     "writer-key",
			Enabled: true,
			Roles:   []string{"writer"},
		},
	}
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

	keys := []KeyEntry{
		{
			ID: "auto-client", Key: "auto-key-123", Enabled: true,
			CreatedAt: time.Now(), WriteApproval: ApprovalAuto,
		},
	}
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

	keys := []KeyEntry{
		{
			ID: "strict-client", Key: "strict-key-123", Enabled: true,
			CreatedAt: time.Now(), WriteApproval: ApprovalRequired,
		},
	}

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

	keys := []KeyEntry{
		{ID: "write-client", Key: "write-key-789", Enabled: true, CreatedAt: time.Now()},
	}

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
