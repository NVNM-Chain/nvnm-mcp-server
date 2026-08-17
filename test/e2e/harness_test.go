// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

// Package e2e holds true end-to-end tests: they drive a real, already
// running NVNM Chain MCP server over real HTTP, using the official MCP
// SDK client, against the real testnet chain behind it.
//
// Nothing here is mocked and nothing is started in-process. Every
// assertion goes through session.CallTool, which is the exact path an
// external agent uses. That is the difference between this package and
// internal/mcp/server_e2e_test.go, which exercises the real MCP protocol
// stack but wires mock evm/anchor clients underneath.
//
// Transaction signing happens locally: the server never receives a
// private key, matching the prepare-sign-submit contract the anchor
// tools document. Only the resulting signed hex is sent, via
// evm_send_raw_transaction.
//
// See README.md in this directory for how to run these.
package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
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
	// envServerURL overrides the MCP server endpoint under test.
	envServerURL = "NVNM_MCP_TEST_SERVER_URL"
	// defaultServerURL is the hosted NVNM Chain MCP testnet deployment
	// (see README.md / docs/DESIGN.md for the endpoint table).
	defaultServerURL = "https://mcp-testnet.nvnmchain.io"

	// envAPIKey, when set, is sent as a Bearer token on every request.
	// Leave unset against a keyless deployment.
	envAPIKey = "NVNM_MCP_TEST_API_KEY"

	// envCredentials overrides the path to the signing credentials file.
	envCredentials = "NVNM_MCP_TEST_CREDENTIALS"
	// defaultCredentialsPath is the git-ignored credentials file at the
	// repository root, the same one the integration tests and
	// cmd/seed-test-data read.
	defaultCredentialsPath = "../../.chain_credentials.txt"

	// httpTimeout bounds a single MCP request.
	httpTimeout = 30 * time.Second
	// receiptTimeout bounds the wait for one transaction receipt. The
	// testnet RPC can take >30s to surface a receipt for a transaction
	// already on chain (slow comet-side indexing on busy blocks).
	receiptTimeout = 90 * time.Second
	// receiptPollInterval is the gap between receipt polls.
	receiptPollInterval = 2 * time.Second

	// challengeVersionTag mirrors the tag documented in the
	// nvnm_setup_verify_* tool descriptions. Recomputing the challenge
	// locally (rather than only echoing the server's) is what makes the
	// verify tests meaningful: they assert the documented derivation is
	// what the server actually implements.
	challengeVersionTag = "nvnm-setup-challenge-v1"
)

// flow is the shared state threaded through the ordered phases of the
// full-coverage run. Each phase reads what earlier phases produced (a
// registry ID, a record, a block number) and records what later phases
// need, so the whole suite is one causally-linked journey rather than a
// bag of independent probes.
type flow struct {
	session *mcp.ClientSession

	// Signing identity, loaded from the credentials file.
	address string
	key     *defiwallet.PrivateKey

	// Chain identity, as reported by the server itself rather than
	// hardcoded, so the suite follows whatever deployment it is pointed at.
	chainID       int64
	anchorAddress string

	// Artifacts created during the run.
	registryName string
	registryID   uint64
	checksum     string
	uri          string
	recordID     uint64
	recordIndex  uint64

	// Chain coordinates of the addRecord transaction, reused by the
	// evm_get_block / evm_get_logs / evm_get_transaction phases so those
	// tools are exercised against data this run actually produced.
	recordTxHash    string
	recordBlockNum  uint64
	recordBlockHash string

	// advertisedTools is the server's tools/list response, and calledTools
	// records every tool this run invoked. The final phase compares them,
	// which is what makes "covers every tool" an assertion rather than a
	// claim.
	advertisedTools []string
	calledTools     map[string]bool

	// Preconditions for the write half of the run, recorded by the phases
	// that discover them. They are flags rather than a t.Skip inside the
	// subtest because skipping a subtest does not stop the parent: t.Run
	// reports a skipped subtest as success, so the run would sail on into
	// writes that cannot possibly work. The parent checks these instead.
	writeToolsAvailable bool
	walletFunded        bool
}

// newFlow connects to the MCP server under test and loads the signing
// credentials. It skips (rather than fails) when credentials are absent,
// so the suite stays runnable on a machine without testnet keys.
func newFlow(t *testing.T) *flow {
	t.Helper()

	address, key := loadCredentials(t)
	serverURL := serverURL()
	t.Logf("MCP server under test: %s", serverURL)

	httpClient := &http.Client{
		Transport: &bearerTransport{token: os.Getenv(envAPIKey)},
		Timeout:   httpTimeout,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "nvnm-mcp-e2e", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   serverURL,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server %s: %v", serverURL, err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return &flow{
		session:     session,
		address:     address,
		key:         key,
		calledTools: make(map[string]bool),
	}
}

func serverURL() string {
	if v := os.Getenv(envServerURL); v != "" {
		return v
	}
	return defaultServerURL
}

func credentialsPath() string {
	if v := os.Getenv(envCredentials); v != "" {
		return v
	}
	return defaultCredentialsPath
}

// bearerTransport injects an Authorization header when a key is
// configured, and is a pass-through when it is not.
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

// loadCredentials reads the Address + PrivateKey lines from the
// git-ignored credentials file.
func loadCredentials(t *testing.T) (address string, key *defiwallet.PrivateKey) {
	t.Helper()

	path := credentialsPath()
	data, err := os.ReadFile(path) //nolint:gosec // test fixture path, operator-controlled
	if err != nil {
		t.Skipf("credentials file not found (%s): %v", path, err)
	}

	var keyHex string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Address:"); ok {
			address = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "PrivateKey:"); ok {
			keyHex = strings.TrimSpace(after)
		}
	}
	if address == "" || keyHex == "" {
		t.Fatalf("credentials file %s is missing an Address or PrivateKey line", path)
	}

	keyBytes, err := hex.DecodeString(strings.TrimPrefix(keyHex, "0x"))
	if err != nil {
		t.Fatalf("invalid private key hex in %s: %v", path, err)
	}
	key = defiwallet.NewKeyFromBytes(keyBytes)

	// Trust the key, not the file's Address line, and fail loudly if they
	// disagree. A drifted credentials file otherwise surfaces much later as
	// a baffling nonce or permission error deep in the write flow.
	derived := key.Address()
	if !strings.EqualFold(strings.TrimSpace(address), derived.String()) {
		t.Fatalf("credentials file %s is inconsistent: Address says %s but PrivateKey derives %s",
			path, address, derived.String())
	}

	// Use the canonical rendering from here on so challenge derivation and
	// address comparisons never hinge on how the file was formatted.
	return derived.String(), key
}

// --- Tool invocation ---

// call invokes an MCP tool and records it for the coverage phase. A
// transport-level error is fatal; a tool-level error (IsError) is
// returned to the caller so negative-path assertions stay possible.
func (f *flow) call(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	f.calledTools[tool] = true

	result, err := f.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      tool,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): transport error: %v", tool, err)
	}
	return result
}

// tryCall is call without the assertion that the transport survived. It
// returns the transport error instead of failing on it, which only a
// phase probing how the server behaves under a pathological input needs
// -- a request that never comes back is a finding to report, not a
// reason to abort the suite.
func (f *flow) tryCall(
	t *testing.T, tool string, args map[string]any,
) (*mcp.CallToolResult, error) {
	t.Helper()

	f.calledTools[tool] = true
	return f.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      tool,
		Arguments: args,
	})
}

// callOK invokes a tool, asserts it did not return a tool-level error,
// and decodes its structured content into out (which may be nil when
// only the success itself matters).
func (f *flow) callOK(t *testing.T, tool string, args map[string]any, out any) {
	t.Helper()

	result := f.call(t, tool, args)
	if result.IsError {
		t.Fatalf("%s returned a tool error: %s", tool, resultText(result))
	}
	if out != nil {
		decodeStructured(t, tool, result, out)
	}
}

// decodeStructured round-trips a tool's structured content through JSON
// into out. Decoding from the wire (rather than reusing the server's own
// Go types, which are unexported anyway) is deliberate: it asserts the
// published JSON contract, not an internal struct.
func decodeStructured(t *testing.T, tool string, result *mcp.CallToolResult, out any) {
	t.Helper()

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal structured content: %v", tool, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: decode structured content: %v (raw: %s)", tool, err, raw)
	}
}

// resultText concatenates the text content of a result, which is where
// the SDK puts a tool handler's error message.
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

// --- Prepare / sign / broadcast / confirm ---
//
// The wire shapes these work with (unsignedTx, receipt) live in
// wire_test.go with the rest of the published JSON contract.

// prepare drives one of the anchor_prepare_* tools and returns the
// unsigned transaction it produced. A tool-level rejection is fatal.
func (f *flow) prepare(t *testing.T, tool string, args map[string]any) *unsignedTx {
	t.Helper()

	utx, errText := f.tryPrepare(t, tool, args)
	if utx == nil {
		t.Fatalf("%s returned a tool error: %s", tool, errText)
	}
	return utx
}

// tryPrepare is prepare without the demand that the tool accept the call:
// it returns the unsigned transaction on success, or nil plus the tool's
// error text when the tool rejected it. Phases probing behavior the docs
// do not pin down -- whether the precompile accepts an explicit historical
// version index, say -- need to observe a rejection and classify it rather
// than die on it.
//
// A rejection here is usually a gas-estimation revert: anchor_prepare_*
// estimates gas, which runs the precompile, so caller-input rejections
// surface at prepare time rather than at broadcast time.
func (f *flow) tryPrepare(t *testing.T, tool string, args map[string]any) (*unsignedTx, string) {
	t.Helper()

	result := f.call(t, tool, args)
	if result.IsError {
		return nil, resultText(result)
	}

	var utx unsignedTx
	decodeStructured(t, tool, result, &utx)

	if utx.RawTx == "" {
		t.Fatalf("%s: raw_tx is empty", tool)
	}
	if utx.Gas == 0 {
		t.Fatalf("%s: gas must be > 0", tool)
	}
	if utx.ChainID != f.chainID {
		t.Errorf("%s: chain_id = %d, want %d (as reported by evm_get_chain_id)", tool, utx.ChainID, f.chainID)
	}
	if !strings.EqualFold(utx.To, f.anchorAddress) {
		t.Errorf("%s: to = %s, want the anchor precompile %s", tool, utx.To, f.anchorAddress)
	}
	t.Logf("  prepared %s: type=%d nonce=%d gas=%d", tool, utx.Type, utx.Nonce, utx.Gas)
	return &utx, ""
}

// assertUnsignedTxShape checks the envelope every anchor_prepare_* tool
// promises, so each prepare phase can assert its own arguments and lean
// on this for the rest. It is deliberately strict about the two
// encodings: unsignedTx carries decimal strings, wallet_tx_request the
// same values as 0x-hex quantities, and a caller pasting the wrong one
// into a wallet gets a transaction that silently means something else.
func assertUnsignedTxShape(t *testing.T, f *flow, utx *unsignedTx) {
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

	// The browser-wallet form of the same transaction. That path is the
	// one no test process can drive end to end, which is exactly why its
	// shape is worth pinning here.
	w := utx.WalletTxRequest
	if w == nil {
		t.Fatal("wallet_tx_request is absent; the browser-wallet signing path has nothing to hand to a wallet")
	}
	if !strings.EqualFold(w.From, f.address) {
		t.Errorf("wallet_tx_request.from = %s, want %s", w.From, f.address)
	}
	if !strings.EqualFold(w.To, utx.To) {
		t.Errorf("wallet_tx_request.to = %s, but the unsigned tx targets %s", w.To, utx.To)
	}
	if w.Data != utx.Data {
		t.Error("wallet_tx_request.data differs from the unsigned tx calldata; the two signing " +
			"paths would produce different transactions")
	}
	// The gas-pricing fields track the transaction type: a type-2 request
	// carries the EIP-1559 pair and omits gasPrice.
	for field, value := range map[string]string{
		"value":                w.Value,
		"chainId":              w.ChainID,
		"gas":                  w.Gas,
		"maxFeePerGas":         w.MaxFeePerGas,
		"maxPriorityFeePerGas": w.MaxPriorityFeePerGas,
	} {
		if !strings.HasPrefix(value, "0x") {
			t.Errorf("wallet_tx_request.%s = %q, want a 0x-hex quantity (EIP-1193 takes hex, "+
				"not the decimal strings on the unsigned tx)", field, value)
		}
	}
	if w.GasPrice != "" {
		t.Errorf("wallet_tx_request.gasPrice = %q on a type-2 transaction; a wallet given both "+
			"pricing schemes has to guess which one the caller meant", w.GasPrice)
	}
}

// signBroadcastConfirm signs an unsigned transaction with the local key,
// broadcasts it via evm_send_raw_transaction, then polls
// evm_get_transaction_receipt until it is mined. It fails unless the
// transaction lands with status "success".
func (f *flow) signBroadcastConfirm(t *testing.T, utx *unsignedTx) *receipt {
	t.Helper()

	r := f.signBroadcastAllowRevert(t, utx)
	if r.Status != "success" {
		t.Fatalf("transaction %s did not succeed: status=%s", r.TxHash, r.Status)
	}
	return r
}

// signBroadcastAllowRevert is signBroadcastConfirm without the demand that
// the transaction succeed: it returns the receipt whatever its status, so
// a caller can treat an on-chain revert as an outcome to classify. A
// transport failure or a receipt that never appears is still fatal.
func (f *flow) signBroadcastAllowRevert(t *testing.T, utx *unsignedTx) *receipt {
	t.Helper()

	signedHex := f.sign(t, utx)

	var sent struct {
		TxHash string `json:"tx_hash"`
	}
	f.callOK(t, "evm_send_raw_transaction", map[string]any{"signed_tx": signedHex}, &sent)
	if sent.TxHash == "" {
		t.Fatal("evm_send_raw_transaction returned an empty tx_hash")
	}
	t.Logf("  broadcast: tx=%s", sent.TxHash)

	return f.pollReceipt(t, sent.TxHash)
}

// sign decodes the RLP raw_tx, signs it locally, and returns the
// 0x-prefixed signed hex. This is the headless-signer path; the browser
// path (wallet_tx_request) is not exercisable from a test process.
func (f *flow) sign(t *testing.T, utx *unsignedTx) string {
	t.Helper()
	return f.signDecoded(t, f.decodeUnsigned(t, utx))
}

// signTo re-points an unsigned transaction at a different destination
// before signing it. Only the relay-scope check in
// evm_send_raw_transaction_test.go needs this: proving the relay refuses
// a non-anchor destination takes a validly signed transaction that has
// one, and nothing else can produce that.
//
// The result carries value=0, so even if a future change let it through
// it would move no funds.
func (f *flow) signTo(t *testing.T, utx *unsignedTx, to string) string {
	t.Helper()

	toAddr, err := defitypes.AddressFromHex(to)
	if err != nil {
		t.Fatalf("destination %q: %v", to, err)
	}
	tx := f.decodeUnsigned(t, utx)
	tx.SetTo(toAddr)
	tx.SetValue(big.NewInt(0))
	return f.signDecoded(t, tx)
}

// decodeUnsigned turns the RLP raw_tx back into a transaction and pins
// the chain ID, which the RLP of an unsigned type-2 transaction does not
// always carry through the round trip.
func (f *flow) decodeUnsigned(t *testing.T, utx *unsignedTx) *defitypes.Transaction {
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
	return tx
}

func (f *flow) signDecoded(t *testing.T, tx *defitypes.Transaction) string {
	t.Helper()

	if signErr := f.key.SignTransaction(context.Background(), tx); signErr != nil {
		t.Fatalf("sign tx: %v", signErr)
	}
	signedBytes, err := tx.Raw()
	if err != nil {
		t.Fatalf("encode signed tx: %v", err)
	}
	return "0x" + hex.EncodeToString(signedBytes)
}

// pollReceipt polls evm_get_transaction_receipt (the real tool, not a
// local chain client) until the transaction is mined, and returns the
// receipt whatever status it carries. Judging that status is the caller's
// job.
func (f *flow) pollReceipt(t *testing.T, txHash string) *receipt {
	t.Helper()

	deadline := time.Now().Add(receiptTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(receiptPollInterval)

		result := f.call(t, "evm_get_transaction_receipt", map[string]any{"tx_hash": txHash})
		if result.IsError {
			// Not yet indexed: the tool reports not-found until inclusion.
			continue
		}

		var r receipt
		decodeStructured(t, "evm_get_transaction_receipt", result, &r)
		t.Logf("  mined: status=%s block=%d gasUsed=%d", r.Status, r.BlockNumber, r.GasUsed)
		return &r
	}

	t.Fatalf("timed out after %s waiting for a receipt for %s", receiptTimeout, txHash)
	return nil
}

// --- Misc helpers ---

// challengeFor recomputes the nvnm_setup_verify_* challenge for an
// address using the derivation published in those tools' descriptions.
func challengeFor(address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(address) + ":" + challengeVersionTag))
	return "0x" + hex.EncodeToString(sum[:])
}

// sha256Hex returns the 0x-prefixed SHA-256 digest of s, which is what
// nvnm_setup_verify_hash expects over the challenge string.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "0x" + hex.EncodeToString(sum[:])
}

// uniqueSuffix produces a collision-resistant suffix for on-chain names
// so repeated runs never collide with each other.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
