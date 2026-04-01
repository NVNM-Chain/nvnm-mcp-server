package mcp

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

// buildSignedTxHex creates a valid signed transaction hex for testing.
func buildSignedTxHex(t *testing.T) string {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return signTxWithKey(t, key)
}

func signTxWithKey(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	to := common.HexToAddress("0x0000000000000000000000000000000000000A00")
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     []byte{0xca, 0xfe},
	})
	signer := types.NewEIP155Signer(big.NewInt(58887))
	signed, err := types.SignTx(tx, signer, key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal tx: %v", err)
	}
	return "0x" + common.Bytes2Hex(raw)
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

	srv := NewServer(cfg.evmClient, anchorClient, true, approval, nil, testLogger())

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
	summary, err := DecodeTxSummary(signedTx, "test-client")
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
	_, err := DecodeTxSummary("0xZZZZ", "client")
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

	var ks *KeyStore
	if len(cfg.keys) > 0 {
		ks = NewKeyStore(cfg.keys)
	}

	srv := NewServer(evmClient, anchorClient, true, approval, nil, logger)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv.mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	var handler http.Handler = mcpHandler
	if ks != nil && !ks.Empty() {
		handler = APIKeyAuth(mcpHandler, ks, logger)
	}

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
	if !strings.Contains(capturedMessage, "Client:   write-client") {
		t.Errorf("approval prompt should contain client identity, got:\n%s", capturedMessage)
	}
}
