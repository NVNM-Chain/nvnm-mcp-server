package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func startTestServer(t *testing.T) *mcp.ClientSession {
	t.Helper()

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

	srv := NewServer(evmClient, anchorClient, testServerConfig(true, ApprovalRequired), nil, testLogger())

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

func TestE2E_ListTools_Returns21(t *testing.T) {
	session := startTestServer(t)

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// 16 pre-8.8 tools + 5 onboarding tools registered by Phase 8.8.
	const want = 21
	if len(result.Tools) != want {
		names := make([]string, len(result.Tools))
		for i, tool := range result.Tools {
			names[i] = tool.Name
		}
		t.Errorf("got %d tools, want %d: %v", len(result.Tools), want, names)
	}
}

func TestE2E_ListTools_ContainsExpectedNames(t *testing.T) {
	session := startTestServer(t)

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	expected := map[string]bool{
		// Phase 8.8 onboarding tools.
		"nvnm_overview":               false,
		"wallet_status":               false,
		"nvnm_setup_wizard":           false,
		"nvnm_setup_verify_hash":      false,
		"nvnm_setup_verify_signature": false,
		// EVM reads.
		"evm_get_chain_id":            false,
		"evm_get_block":               false,
		"evm_get_transaction":         false,
		"evm_get_transaction_receipt": false,
		"evm_get_balance":             false,
		"evm_get_code":                false,
		"evm_get_logs":                false,
		"evm_call_contract":           false,
		// EVM write.
		"evm_send_raw_transaction": false,
		// Anchor reads.
		"anchor_info":           false,
		"anchor_get_registry":   false,
		"anchor_get_registries": false,
		"anchor_get_records":    false,
		// Anchor writes.
		"anchor_prepare_add_registry": false,
		"anchor_prepare_add_record":   false,
		"anchor_prepare_grant_role":   false,
	}

	for _, tool := range result.Tools {
		if _, ok := expected[tool.Name]; ok {
			expected[tool.Name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestE2E_CallTool_ChainID(t *testing.T) {
	session := startTestServer(t)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_chain_id",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content in response")
	}
}

func TestE2E_CallTool_AnchorInfo(t *testing.T) {
	session := startTestServer(t)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "anchor_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content in response")
	}
}

func TestE2E_CallTool_InvalidAddress(t *testing.T) {
	session := startTestServer(t)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "evm_get_balance",
		Arguments: map[string]any{"address": "not-a-valid-address"},
	})

	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for invalid address")
	}
}

func TestE2E_CallTool_MissingRegistryIDAndName(t *testing.T) {
	session := startTestServer(t)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "anchor_get_registry",
		Arguments: map[string]any{},
	})

	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when neither id nor name provided")
	}
}
