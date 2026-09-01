// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	envServerURL           = "NVNM_MCP_TEST_SERVER_URL"
	envAPIKey              = "NVNM_MCP_TEST_API_KEY"        //nolint:gosec // pragma: allowlist secret -- env var name, no key material
	envCredentials         = "NVNM_MCP_TEST_CREDENTIALS"    //nolint:gosec
	defaultCredentialsPath = "../../.chain_credentials.txt" //nolint:gosec

	HTTPTimeout = 30 * time.Second
	// RegistriesTimeout covers anchor_get_registries listing: the server
	// pages the full registry table over RPC and paginates in memory, which
	// commonly takes 20-30s on a populated chain.
	RegistriesTimeout = 90 * time.Second
	// RegistriesLatencyBudget is how long a successful by-name listing may
	// take before the hot path treats the scan as degraded. The HTTP wait
	// stays RegistriesTimeout so a slow-but-healthy scan still completes.
	RegistriesLatencyBudget = 60 * time.Second
	receiptTimeout          = 90 * time.Second
	receiptPollInterval     = 2 * time.Second
)

// Flow is the shared state for TestE2E_HotPath_AnchorDocument.
type Flow struct {
	Session *mcp.ClientSession
	Address string
	Key     *defiwallet.PrivateKey

	ChainID       int64
	AnchorAddress string
	RegistryName  string
	RegistryID    uint64
	Checksum      string
	URI           string
	RecordID      uint64
	RecordIndex   uint64
	RecordTxHash  string
	RecordBlock   uint64

	WriteToolsAvailable     bool
	LifecycleToolsAvailable bool
	WalletFunded            bool
	MainnetReadOnly         bool
}

func NewFlow(t *testing.T) *Flow {
	t.Helper()

	t.Log("loading credentials and starting MCP target")
	address, key := LoadCredentials(t)
	serverURL := StartTarget(t)
	session := ConnectSession(t, serverURL, os.Getenv(envAPIKey))

	return &Flow{
		Session: session,
		Address: address,
		Key:     key,
	}
}

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

func (f *Flow) Call(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), toolCallTimeout(tool))
	defer cancel()
	result, err := f.Session.CallTool(ctx, &mcp.CallToolParams{
		Name:      tool,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): transport error: %v", tool, err)
	}
	return result
}

func toolCallTimeout(tool string) time.Duration {
	if tool == "anchor_get_registries" {
		return RegistriesTimeout
	}
	return HTTPTimeout
}

func (f *Flow) CallOK(t *testing.T, tool string, args map[string]any, out any) {
	t.Helper()

	result := f.Call(t, tool, args)
	if result.IsError {
		t.Fatalf("%s returned a tool error: %s", tool, ResultText(result))
	}
	if out != nil {
		DecodeStructured(t, tool, result, out)
	}
}

func DecodeStructured(t *testing.T, tool string, result *mcp.CallToolResult, out any) {
	t.Helper()

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal structured content: %v", tool, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: decode structured content: %v (raw: %s)", tool, err, raw)
	}
}

func ResultText(result *mcp.CallToolResult) string {
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

func (f *Flow) Prepare(t *testing.T, tool string, args map[string]any) *UnsignedTx {
	t.Helper()

	result := f.Call(t, tool, args)
	if result.IsError {
		t.Fatalf("%s returned a tool error: %s", tool, ResultText(result))
	}

	var utx UnsignedTx
	DecodeStructured(t, tool, result, &utx)
	if utx.RawTx == "" {
		t.Fatalf("%s: raw_tx is empty", tool)
	}
	if utx.Gas == 0 {
		t.Fatalf("%s: gas must be > 0", tool)
	}
	if utx.ChainID != f.ChainID {
		t.Errorf("%s: chain_id = %d, want %d (as reported by nvnm_overview)", tool, utx.ChainID, f.ChainID)
	}
	if !strings.EqualFold(utx.To, f.AnchorAddress) {
		t.Errorf("%s: to = %s, want the anchor precompile %s", tool, utx.To, f.AnchorAddress)
	}
	t.Logf("  prepared %s: type=%d nonce=%d gas=%d", tool, utx.Type, utx.Nonce, utx.Gas)
	return &utx
}

func AssertUnsignedTxShape(t *testing.T, f *Flow, utx *UnsignedTx) {
	t.Helper()

	if !strings.HasPrefix(utx.RawTx, "0x") {
		t.Errorf("raw_tx = %q, want a 0x-prefixed RLP hex string", utx.RawTx)
	}
	if !strings.HasPrefix(utx.Data, "0x") || len(utx.Data) < 10 {
		t.Errorf("data = %q, want 0x + at least a 4-byte selector", utx.Data)
	}
	if utx.Type != 2 {
		t.Errorf("type = %d, want 2 (EIP-1559 is the default since Phase 8.4)", utx.Type)
	}
	if utx.Value != "0" {
		t.Errorf("value = %q, want %q; an anchor call never transfers native funds", utx.Value, "0")
	}
	if utx.MaxFeePerGas == "" || utx.MaxPriorityFeePerGas == "" {
		t.Error("a type-2 transaction must carry both max_fee_per_gas and max_priority_fee_per_gas")
	}
	if utx.GasPrice != utx.MaxFeePerGas {
		t.Errorf("gas_price = %q but max_fee_per_gas = %q; gas_price is dual-populated with "+
			"max_fee_per_gas so a legacy signer still has a usable value", utx.GasPrice, utx.MaxFeePerGas)
	}

	w := utx.WalletTxRequest
	if w == nil {
		t.Fatal("wallet_tx_request is absent; the browser-wallet signing path has nothing to hand to a wallet")
	}
	if !strings.EqualFold(w.From, f.Address) {
		t.Errorf("wallet_tx_request.from = %s, want %s", w.From, f.Address)
	}
	if !strings.EqualFold(w.To, utx.To) {
		t.Errorf("wallet_tx_request.to = %s, but the unsigned tx targets %s", w.To, utx.To)
	}
	if w.Data != utx.Data {
		t.Error("wallet_tx_request.data differs from the unsigned tx calldata; the two signing " +
			"paths would produce different transactions")
	}
	if w.Gas != hexQuantityUint(utx.Gas) {
		t.Errorf("wallet_tx_request.gas = %q, want %s (hex of gas %d)",
			w.Gas, hexQuantityUint(utx.Gas), utx.Gas)
	}
	if w.ChainID != hexQuantityInt64(utx.ChainID) {
		t.Errorf("wallet_tx_request.chainId = %q, want hex of chain_id %d", w.ChainID, utx.ChainID)
	}
	if w.MaxFeePerGas != hexQuantityDecimal(t, utx.MaxFeePerGas) {
		t.Errorf("wallet_tx_request.maxFeePerGas = %q, want hex of %s", w.MaxFeePerGas, utx.MaxFeePerGas)
	}
	if w.MaxPriorityFeePerGas != hexQuantityDecimal(t, utx.MaxPriorityFeePerGas) {
		t.Errorf("wallet_tx_request.maxPriorityFeePerGas = %q, want hex of %s",
			w.MaxPriorityFeePerGas, utx.MaxPriorityFeePerGas)
	}
	if w.Value != "0x0" {
		t.Errorf("wallet_tx_request.value = %q, want 0x0", w.Value)
	}
	if w.GasPrice != "" {
		t.Errorf("wallet_tx_request.gasPrice = %q on a type-2 transaction; a wallet given both "+
			"pricing schemes has to guess which one the caller meant", w.GasPrice)
	}
}

func (f *Flow) SignBroadcastConfirm(t *testing.T, utx *UnsignedTx) *Receipt {
	t.Helper()

	signedHex := f.sign(t, utx)

	var sent struct {
		TxHash string `json:"tx_hash"`
	}
	f.CallOK(t, "evm_send_raw_transaction", map[string]any{"signed_tx": signedHex}, &sent)
	if sent.TxHash == "" {
		t.Fatal("evm_send_raw_transaction returned an empty tx_hash")
	}
	t.Logf("  broadcast: tx=%s", sent.TxHash)

	r := f.pollReceipt(t, sent.TxHash)
	if r.Status != "success" {
		t.Fatalf("transaction %s did not succeed: status=%s", r.TxHash, r.Status)
	}
	return r
}

func (f *Flow) sign(t *testing.T, utx *UnsignedTx) string {
	t.Helper()

	txBytes, err := hex.DecodeString(strings.TrimPrefix(utx.RawTx, "0x"))
	if err != nil {
		t.Fatalf("decode raw_tx hex: %v", err)
	}
	tx := defitypes.NewTransaction()
	if _, decErr := tx.DecodeRLP(txBytes); decErr != nil {
		t.Fatalf("decode unsigned tx RLP: %v", decErr)
	}
	if utx.ChainID < 0 {
		t.Fatalf("negative chain_id %d", utx.ChainID)
	}
	tx.SetChainID(uint64(utx.ChainID))

	if signErr := f.Key.SignTransaction(context.Background(), tx); signErr != nil {
		t.Fatalf("sign tx: %v", signErr)
	}
	signedBytes, err := tx.Raw()
	if err != nil {
		t.Fatalf("encode signed tx: %v", err)
	}
	return "0x" + hex.EncodeToString(signedBytes)
}

func (f *Flow) pollReceipt(t *testing.T, txHash string) *Receipt {
	t.Helper()

	deadline := time.Now().Add(receiptTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(receiptPollInterval)

		result := f.Call(t, "evm_get_transaction_receipt", map[string]any{"tx_hash": txHash})
		if result.IsError {
			continue
		}

		var r Receipt
		DecodeStructured(t, "evm_get_transaction_receipt", result, &r)
		t.Logf("  mined: status=%s block=%d gasUsed=%d", r.Status, r.BlockNumber, r.GasUsed)
		return &r
	}

	t.Fatalf("timed out after %s waiting for a receipt for %s", receiptTimeout, txHash)
	return nil
}

func UniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
