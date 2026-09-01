// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

// MCP tools tests. No //go:build integration tag: these use mock chain
// clients and must run under go test ./... so a pipeline can catch tool
// regressions without RPC or a wallet. Live-chain client tests next to
// evm/anchor keep the integration build tag.

package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	defiwallet "github.com/defiweb/go-eth/wallet"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// Fixture values for mocked CallTool invocations. They exist so the MCP
// layer can return a structured envelope; they are not chain facts.
const (
	callToolSender    = "0x1234567890abcdef1234567890abcdef12345678"
	callToolAccount   = "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	callToolChecksum  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	callToolSendHash  = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	callToolBlockHash = "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	callToolCallData  = "0x06fdde03"
	callToolGas       = uint64(100000)
	// Distinct dummy selectors so a prepare handler wired to the wrong
	// mock method cannot satisfy every prepare case with one payload.
	callToolSelAddRegistry  = "0xaabb0001"
	callToolSelAddRecord    = "0xaabb0002"
	callToolSelUpdateStatus = "0xaabb0003"
	callToolSelGrantRole    = "0xaabb0004"
	callToolSelRevokeRole   = "0xaabb0005"
	callToolMaxFee          = "2000000000"
	callToolTip             = "1000000000"
	callToolChainID         = int64(58887)
)

// TestMCP_Tools is the MCP tools suite: every advertised tool through
// the official SDK client against mock chain clients. No wallet, no
// RPC, no deployment. That is the tool-regression net a pipeline can
// run (and `go test ./...` already does). Live client tests stay in
// the tagged *_integration_test.go files next to evm/anchor.
// Deployment hot-path stays in tests/e2e.
func TestMCP_Tools(t *testing.T) {
	session := startCallToolServer(t)
	advertised := callToolAdvertised(t, session)

	key := defiwallet.NewRandomKey()
	sigAddr := key.Address()
	challenge := challengeForAddress(sigAddr)
	sig, err := key.SignMessage(ctx, []byte(challenge))
	if err != nil {
		t.Fatalf("SignMessage: %v", err)
	}
	fromAddr, err := parseAddress(callToolSender)
	if err != nil {
		t.Fatalf("from: %v", err)
	}
	signedTx := buildSignedTxHex(t)
	hashOK := expectedHashForChallenge(challengeForAddress(fromAddr))

	cases := []struct {
		name  string
		args  map[string]any
		check func(t *testing.T, raw []byte)
	}{
		{
			name: "nvnm_overview",
			args: map[string]any{},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireOverview](t, raw)
				if out.ChainID != callToolChainID {
					t.Errorf("chain_id = %d, want %d", out.ChainID, callToolChainID)
				}
				if out.ChainEnvironment != "testnet" {
					t.Errorf("chain_environment = %q, want testnet", out.ChainEnvironment)
				}
				assert0xAddress(t, "anchor_precompile", out.AnchorPrecompile)
				if out.ChainName == "" || out.TokenNative == "" {
					t.Errorf("chain_name/token_native empty: name=%q token=%q", out.ChainName, out.TokenNative)
				}
			},
		},
		{
			name: "nvnm_setup_wizard",
			args: map[string]any{"address": callToolSender},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireWizard](t, raw)
				if out.State != WizardStateFundedActive {
					t.Errorf("state = %q, want %q", out.State, WizardStateFundedActive)
				}
				if out.Message == "" {
					t.Error("wizard message empty")
				}
				if out.Wallet == nil {
					t.Fatal("wizard wallet snapshot missing")
				}
				assert0xAddress(t, "wallet.address", out.Wallet.Address)
				if out.Wallet.ChainID != callToolChainID {
					t.Errorf("wallet.chain_id = %d, want %d", out.Wallet.ChainID, callToolChainID)
				}
				if out.Wallet.ChainEnvironment != "testnet" {
					t.Errorf("wallet.chain_environment = %q, want testnet", out.Wallet.ChainEnvironment)
				}
				assertDecimalWei(t, "wallet.balance_wei", out.Wallet.BalanceWei)
			},
		},
		{
			name: "nvnm_setup_verify_hash",
			args: map[string]any{"address": callToolSender, "hash": hashOK},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireVerifyHash](t, raw)
				if !out.OK {
					t.Errorf("ok=false expected=%s got=%s", out.Expected, out.Got)
				}
				assert0xAddress(t, "address", out.Address)
			},
		},
		{
			name: "nvnm_setup_verify_signature",
			args: map[string]any{"address": sigAddr.String(), "signature": sig.String()},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireVerifySignature](t, raw)
				if !out.OK {
					t.Errorf("ok=false recovered=%s", out.Recovered)
				}
				assert0xAddress(t, "address", out.Address)
				assert0xAddress(t, "recovered_address", out.Recovered)
				if !strings.EqualFold(out.Recovered, out.Address) {
					t.Errorf("recovered_address = %s, want %s", out.Recovered, out.Address)
				}
			},
		},
		{
			name: "wallet_status",
			args: map[string]any{"address": callToolSender},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireWalletStatus](t, raw)
				if out.Status != WalletStatusFundedActive {
					t.Errorf("status = %q, want %q", out.Status, WalletStatusFundedActive)
				}
				assert0xAddress(t, "address", out.Address)
				assertDecimalWei(t, "balance_wei", out.BalanceWei)
				if out.ChainID != callToolChainID {
					t.Errorf("chain_id = %d, want %d", out.ChainID, callToolChainID)
				}
				if out.ChainEnvironment != "testnet" {
					t.Errorf("chain_environment = %q, want testnet", out.ChainEnvironment)
				}
				if out.Nonce != 3 {
					t.Errorf("nonce = %d, want 3", out.Nonce)
				}
			},
		},
		{
			name: "evm_get_chain_id",
			args: map[string]any{},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireChainID](t, raw)
				if out.ChainID != callToolChainID {
					t.Errorf("chain_id = %d, want %d", out.ChainID, callToolChainID)
				}
				if out.LatestBlockNumber != 100 {
					t.Errorf("latest_block_number = %d, want 100", out.LatestBlockNumber)
				}
			},
		},
		{
			name: "evm_get_block",
			args: map[string]any{"block_number": float64(42)},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireBlock](t, raw)
				if out.Number != 42 {
					t.Errorf("number = %d, want 42", out.Number)
				}
				assert0xHash(t, "hash", out.Hash)
				assert0xHash(t, "parent_hash", out.ParentHash)
				assert0xAddress(t, "miner", out.Miner)
			},
		},
		{
			name: "evm_get_transaction",
			args: map[string]any{"tx_hash": testTxHash},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireTransaction](t, raw)
				if out.Hash != testTxHash {
					t.Errorf("hash = %q", out.Hash)
				}
				assert0xAddress(t, "from", out.From)
				if out.To == nil {
					t.Fatal("to missing")
				}
				assert0xAddress(t, "to", *out.To)
				if out.BlockNumber == nil || *out.BlockNumber != 42 {
					t.Errorf("block_number = %v, want 42", out.BlockNumber)
				}
				if out.BlockHash == nil {
					t.Fatal("block_hash missing")
				}
				assert0xHash(t, "block_hash", *out.BlockHash)
				if out.Nonce != 7 {
					t.Errorf("nonce = %d, want 7", out.Nonce)
				}
				if out.Value != "0" {
					t.Errorf("value = %q, want 0", out.Value)
				}
			},
		},
		{
			name: "evm_get_transaction_receipt",
			args: map[string]any{"tx_hash": testTxHash},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireReceipt](t, raw)
				if out.Status != "success" {
					t.Errorf("status = %q, want success", out.Status)
				}
				assert0xHash(t, "tx_hash", out.TxHash)
				assert0xHash(t, "block_hash", out.BlockHash)
				if out.BlockNumber == 0 {
					t.Error("block_number is 0")
				}
				if out.Logs == nil {
					t.Fatal("logs missing from receipt")
				}
			},
		},
		{
			name: "evm_get_balance",
			args: map[string]any{"address": callToolSender},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireBalance](t, raw)
				assert0xAddress(t, "address", out.Address)
				assertDecimalWei(t, "wei", out.Wei)
				if out.Ether == "" {
					t.Error("ether empty")
				}
			},
		},
		{
			name: "evm_get_code",
			args: map[string]any{"address": testAddr},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireCode](t, raw)
				assert0xAddress(t, "address", out.Address)
			},
		},
		{
			name: "evm_get_logs",
			args: map[string]any{"address": testAddr},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireLogs](t, raw)
				if out.Count != 1 || len(out.Logs) != 1 {
					t.Fatalf("count = %d logs = %d, want 1", out.Count, len(out.Logs))
				}
				assert0xAddress(t, "logs[0].address", out.Logs[0].Address)
				assert0xHash(t, "logs[0].tx_hash", out.Logs[0].TxHash)
				if len(out.Logs[0].Data) < 2 || (out.Logs[0].Data[:2] != "0x" && out.Logs[0].Data[:2] != "0X") {
					t.Errorf("logs[0].data = %q, want 0x-prefixed hex", out.Logs[0].Data)
				}
			},
		},
		{
			name: "evm_call_contract",
			args: map[string]any{"to": testAddr, "data": callToolCallData},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireCall](t, raw)
				if out.Result != "0xdead" {
					t.Errorf("result = %q, want 0xdead", out.Result)
				}
			},
		},
		{
			name: "evm_send_raw_transaction",
			args: map[string]any{"signed_tx": signedTx},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireSendTx](t, raw)
				if out.TxHash != callToolSendHash {
					t.Errorf("tx_hash = %q", out.TxHash)
				}
				assert0xHash(t, "tx_hash", out.TxHash)
			},
		},
		{
			name: "anchor_info",
			args: map[string]any{},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireAnchorInfo](t, raw)
				assert0xAddress(t, "address", out.Address)
				if out.ChainID != callToolChainID {
					t.Errorf("chain_id = %d, want %d", out.ChainID, callToolChainID)
				}
				if !out.ABILoaded {
					t.Error("abi_loaded = false")
				}
				if out.MethodCount != 5 {
					t.Errorf("method_count = %d, want 5", out.MethodCount)
				}
			},
		},
		{
			name: "anchor_get_registry",
			args: map[string]any{"id": float64(1)},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireRegistry](t, raw)
				if out.ID != 1 {
					t.Errorf("id = %d, want 1", out.ID)
				}
				if out.Name != "docs" {
					t.Errorf("name = %q, want docs", out.Name)
				}
				if out.ContentTrust == "" {
					t.Fatal("content_trust empty")
				}
				assert0xAddress(t, "creator", out.Creator)
			},
		},
		{
			name: "anchor_get_registries",
			args: map[string]any{"registry_id": float64(1)},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireRegistries](t, raw)
				if len(out.Registries) == 0 {
					t.Fatal("registries empty")
				}
				if out.ContentTrust == "" {
					t.Fatal("content_trust empty")
				}
				assert0xAddress(t, "registries[0].creator", out.Registries[0].Creator)
				if out.Pagination == nil || out.Pagination.NextKey == "" {
					t.Fatal("pagination.next_key empty; cursor must be on the wire so schema " +
						"validation actually runs (a missing cursor hid the []byte/string bug)")
				}
			},
		},
		{
			name: "anchor_get_records",
			args: map[string]any{"registry_id": float64(1)},
			check: func(t *testing.T, raw []byte) {
				out := decodeWire[wireRecords](t, raw)
				if len(out.Records) == 0 {
					t.Fatal("records empty")
				}
				if out.ContentTrust == "" {
					t.Fatal("content_trust empty")
				}
				rec := out.Records[0]
				assertChecksum64(t, "checksum", rec.Checksum)
				assertRecordStatus(t, rec.Status)
				if rec.ChecksumAlgo != "sha256" {
					t.Errorf("checksum_algo = %q, want sha256", rec.ChecksumAlgo)
				}
				if rec.RegistryID != 1 || rec.RecordID != 1 || rec.Index != 1 {
					t.Errorf("ids registry=%d record=%d index=%d, want 1/1/1",
						rec.RegistryID, rec.RecordID, rec.Index)
				}
				if rec.URI != "ipfs://doc" {
					t.Errorf("uri = %q, want ipfs://doc", rec.URI)
				}
				if !rec.IsLatest {
					t.Error("is_latest = false")
				}
				if out.Pagination == nil || out.Pagination.NextKey == "" {
					t.Fatal("pagination.next_key empty on records")
				}
			},
		},
		{
			name:  "anchor_prepare_add_registry",
			args:  callToolPrepareRegistryArgs(),
			check: checkCallToolUnsignedTx(callToolSelAddRegistry),
		},
		{
			name:  "anchor_prepare_add_record",
			args:  callToolPrepareRecordArgs(),
			check: checkCallToolUnsignedTx(callToolSelAddRecord),
		},
		{
			name: "anchor_prepare_update_record_status",
			args: map[string]any{
				"from": callToolSender, "registry_id": float64(1),
				"record_id": float64(1), "status": "Superseded",
			},
			check: checkCallToolUnsignedTx(callToolSelUpdateStatus),
		},
		{
			name:  "anchor_prepare_grant_role",
			args:  callToolPrepareRoleArgs(),
			check: checkCallToolUnsignedTx(callToolSelGrantRole),
		},
		{
			name:  "anchor_prepare_revoke_role",
			args:  callToolPrepareRoleArgs(),
			check: checkCallToolUnsignedTx(callToolSelRevokeRole),
		},
	}

	called := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := callToolOK(t, session, tc.name, tc.args)
			called[tc.name] = struct{}{}
			assertCallToolNextActions(t, tc.name, raw, advertised)
			tc.check(t, raw)
		})
	}

	t.Run("anchor_get_registries_by_name", func(t *testing.T) {
		raw := callToolOK(t, session, "anchor_get_registries", map[string]any{
			"name":  "docs",
			"match": "exact",
		})
		assertCallToolNextActions(t, "anchor_get_registries", raw, advertised)
		out := decodeWire[wireRegistries](t, raw)
		if len(out.Registries) != 1 {
			t.Fatalf("registries = %d, want 1", len(out.Registries))
		}
		if out.Registries[0].Name != "docs" || out.Registries[0].ID != 1 {
			t.Errorf("registries[0] = id=%d name=%q, want id=1 name=docs",
				out.Registries[0].ID, out.Registries[0].Name)
		}
		assert0xAddress(t, "registries[0].creator", out.Registries[0].Creator)
		if out.ContentTrust == "" {
			t.Fatal("content_trust empty")
		}
		if out.Pagination == nil || out.Pagination.Total != 1 {
			t.Errorf("pagination.total = %v, want 1", out.Pagination)
		}
	})

	t.Run("coverage", func(t *testing.T) {
		var missed []string
		for name := range advertised {
			if _, ok := called[name]; !ok {
				missed = append(missed, name)
			}
		}
		if len(missed) > 0 {
			t.Errorf("advertised tools with no mocked CallTool invocation: %v", missed)
		}
		t.Logf("covered %d/%d advertised tools", len(called), len(advertised))
	})
}

func startCallToolServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	blockNum := uint64(42)
	idx := uint64(0)
	to := testAddr
	blockHash := callToolBlockHash
	cursor := anchor.EncodeCursor([]byte{0, 0, 0, 0, 0, 0, 0, 101})
	return startTestServerWithConfig(t, e2eServerConfig{
		evmClient: &mockEVM{
			chainInfo: &evm.ChainInfo{ChainID: callToolChainID, LatestBlockNumber: 100},
			balance:   &evm.NormalizedBalance{Address: callToolSender, Wei: "1000000000000000000", Ether: "1"},
			nonce:     3,
			block: &evm.NormalizedBlock{
				Number: 42, Hash: callToolBlockHash, ParentHash: testTxHash, Miner: callToolSender,
			},
			tx: &evm.NormalizedTransaction{
				Hash: testTxHash, From: callToolSender, To: &to, Value: "0",
				Gas: 21000, GasPrice: "1", Data: "0x", Nonce: 7, BlockNumber: &blockNum,
				BlockHash: &blockHash, Index: &idx,
			},
			receipt: &evm.NormalizedReceipt{
				TxHash: testTxHash, BlockNumber: 42, BlockHash: callToolBlockHash,
				Status: "success", GasUsed: 21000, Logs: []evm.NormalizedLog{},
			},
			code:       &evm.CodeResult{Address: testAddr, Bytecode: "0x", IsContract: false},
			logs:       []evm.NormalizedLog{{Address: testAddr, TxHash: testTxHash, Data: "0x"}},
			callResult: []byte{0xde, 0xad},
			sendTxHash: callToolSendHash,
		},
		anchorClient: &mockAnchor{
			info: anchor.PrecompileInfo{
				Address: testAddr, ChainID: callToolChainID, ABILoaded: true, MethodCount: 5,
			},
			registry: &anchor.Registry{ID: 1, Name: "docs", Creator: callToolSender},
			registries: &anchor.GetRegistriesResponse{
				Registries: []anchor.Registry{{ID: 1, Name: "docs", Creator: callToolSender}},
				Pagination: &anchor.PageResponse{Total: 1, NextKey: cursor},
			},
			registriesFn: func(int) (*anchor.GetRegistriesResponse, error) {
				return &anchor.GetRegistriesResponse{
					Registries: []anchor.Registry{{ID: 1, Name: "docs", Creator: callToolSender}},
					Pagination: &anchor.PageResponse{Total: 1},
				}, nil
			},
			records: &anchor.GetRecordsResponse{
				Records: []anchor.Record{{
					RegistryID: 1, RecordID: 1, Index: 1, Checksum: callToolChecksum,
					ChecksumAlgo: "sha256", URI: "ipfs://doc", Status: "Active", IsLatest: true,
				}},
				Pagination: &anchor.PageResponse{Total: 1, NextKey: cursor},
			},
			unsignedTxByMethod: map[string]*anchor.UnsignedTransaction{
				"addRegistry":        callToolMockUnsignedTx(t, callToolSelAddRegistry),
				"addRecord":          callToolMockUnsignedTx(t, callToolSelAddRecord),
				"updateRecordStatus": callToolMockUnsignedTx(t, callToolSelUpdateStatus),
				"grantRole":          callToolMockUnsignedTx(t, callToolSelGrantRole),
				"revokeRole":         callToolMockUnsignedTx(t, callToolSelRevokeRole),
			},
		},
	})
}

func callToolMockUnsignedTx(t *testing.T, data string) *anchor.UnsignedTransaction {
	t.Helper()
	return &anchor.UnsignedTransaction{
		RawTx:                "0x02dead",
		Type:                 2,
		To:                   testAddr,
		Data:                 data,
		Nonce:                1,
		Gas:                  callToolGas,
		GasPrice:             callToolMaxFee,
		MaxFeePerGas:         callToolMaxFee,
		MaxPriorityFeePerGas: callToolTip,
		Value:                "0",
		ChainID:              callToolChainID,
		WalletTxRequest: &anchor.WalletTransactionRequest{
			From:                 callToolSender,
			To:                   testAddr,
			Data:                 data,
			Value:                "0x0",
			ChainID:              int64Hex(callToolChainID),
			Gas:                  uint64Hex(callToolGas),
			MaxFeePerGas:         decimalHex(t, callToolMaxFee),
			MaxPriorityFeePerGas: decimalHex(t, callToolTip),
		},
	}
}

func callToolAdvertised(t *testing.T, session *mcp.ClientSession) map[string]struct{} {
	t.Helper()
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	out := make(map[string]struct{}, len(listed.Tools))
	for _, tool := range listed.Tools {
		out[tool.Name] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatal("tools/list returned no tools")
	}
	return out
}

func callToolOK(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) []byte {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s returned error: %v", name, result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatalf("%s: missing structured content", name)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	return raw
}

func assertCallToolNextActions(t *testing.T, tool string, raw []byte, advertised map[string]struct{}) {
	t.Helper()
	var env struct {
		NextActions []NextAction `json:"next_actions"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode next_actions: %v", err)
	}
	for _, a := range env.NextActions {
		if a.Tool == "" {
			t.Error("next_actions entry with empty tool")
			continue
		}
		if _, ok := advertised[a.Tool]; !ok {
			t.Errorf("next_actions names unknown tool %q", a.Tool)
		}
	}
	if _, allowEmpty := callToolEmptyNext[tool]; allowEmpty {
		return
	}
	if len(env.NextActions) == 0 {
		t.Fatalf("%s: next_actions empty", tool)
	}
	if want, ok := callToolFirstHint[tool]; ok && env.NextActions[0].Tool != want {
		t.Errorf("%s: next_actions[0].tool = %q, want %q", tool, env.NextActions[0].Tool, want)
	}
}

// callToolEmptyNext lists tools whose handlers return no hint by design.
var callToolEmptyNext = map[string]struct{}{
	"evm_get_balance":   {},
	"evm_call_contract": {},
}

// callToolFirstHint pins the first next_actions tool for handlers that
// always emit the same lead hint on the mocked success path.
var callToolFirstHint = map[string]string{
	"nvnm_overview":                       "nvnm_setup_wizard",
	"nvnm_setup_wizard":                   "anchor_get_registries",
	"nvnm_setup_verify_hash":              "nvnm_setup_verify_signature",
	"nvnm_setup_verify_signature":         "wallet_status",
	"wallet_status":                       "anchor_get_registries",
	"evm_get_chain_id":                    "evm_get_block",
	"evm_get_block":                       "evm_get_transaction",
	"evm_get_transaction":                 "evm_get_transaction_receipt",
	"evm_get_transaction_receipt":         "anchor_get_records",
	"evm_get_code":                        "evm_get_balance",
	"evm_get_logs":                        "evm_get_transaction_receipt",
	"evm_send_raw_transaction":            "evm_get_transaction_receipt",
	"anchor_info":                         "anchor_get_registries",
	"anchor_get_registry":                 "anchor_get_records",
	"anchor_get_registries":               "anchor_get_registry",
	"anchor_get_records":                  "anchor_prepare_add_record",
	"anchor_prepare_add_registry":         "evm_send_raw_transaction",
	"anchor_prepare_add_record":           "evm_send_raw_transaction",
	"anchor_prepare_update_record_status": "evm_send_raw_transaction",
	"anchor_prepare_grant_role":           "evm_send_raw_transaction",
	"anchor_prepare_revoke_role":          "evm_send_raw_transaction",
}

func checkCallToolUnsignedTx(wantData string) func(*testing.T, []byte) {
	return func(t *testing.T, raw []byte) {
		t.Helper()
		out := decodeWire[wireUnsignedTx](t, raw)
		if len(out.RawTx) < 4 || (out.RawTx[:2] != "0x" && out.RawTx[:2] != "0X") {
			t.Errorf("raw_tx = %q, want 0x-prefixed hex", out.RawTx)
		}
		assert0xAddress(t, "to", out.To)
		if out.Type != 2 {
			t.Errorf("type = %d, want 2", out.Type)
		}
		if out.Value != "0" {
			t.Errorf("value = %q, want 0", out.Value)
		}
		if out.Data != wantData {
			t.Errorf("data = %q, want %q", out.Data, wantData)
		}
		if out.Nonce != 1 {
			t.Errorf("nonce = %d, want 1", out.Nonce)
		}
		if out.GasPrice != out.MaxFeePerGas {
			t.Errorf("gas_price = %q max_fee_per_gas = %q", out.GasPrice, out.MaxFeePerGas)
		}

		w := out.WalletTxRequest
		if w == nil {
			t.Fatal("wallet_tx_request missing")
		}
		assert0xAddress(t, "wallet_tx_request.from", w.From)
		assert0xAddress(t, "wallet_tx_request.to", w.To)
		if w.Data != out.Data {
			t.Error("wallet_tx_request.data differs from data")
		}
		if w.Value != "0x0" {
			t.Errorf("wallet_tx_request.value = %q, want 0x0", w.Value)
		}
		if w.Gas != uint64Hex(out.Gas) {
			t.Errorf("wallet_tx_request.gas = %q, want %s (hex of gas %d)", w.Gas, uint64Hex(out.Gas), out.Gas)
		}
		if w.ChainID != int64Hex(out.ChainID) {
			t.Errorf("wallet_tx_request.chainId = %q, want hex of chain_id %d", w.ChainID, out.ChainID)
		}
		if w.MaxFeePerGas != decimalHex(t, out.MaxFeePerGas) {
			t.Errorf("wallet_tx_request.maxFeePerGas = %q, want hex of %s", w.MaxFeePerGas, out.MaxFeePerGas)
		}
		if w.MaxPriorityFeePerGas != decimalHex(t, out.MaxPriorityFeePerGas) {
			t.Errorf("wallet_tx_request.maxPriorityFeePerGas = %q, want hex of %s",
				w.MaxPriorityFeePerGas, out.MaxPriorityFeePerGas)
		}
		if w.GasPrice != "" {
			t.Errorf("wallet_tx_request.gasPrice = %q, want empty on type 2", w.GasPrice)
		}
	}
}

func callToolPrepareRegistryArgs() map[string]any {
	return map[string]any{
		"from": callToolSender, "name": "calltool-docs", "description": "mocked calltool registry",
	}
}

func callToolPrepareRecordArgs() map[string]any {
	return map[string]any{
		"from": callToolSender, "registry_id": float64(1), "uri": "ipfs://doc",
		"checksum": callToolChecksum, "checksum_algo": "sha256", "metadata": "coverage",
	}
}

func callToolPrepareRoleArgs() map[string]any {
	return map[string]any{
		"from": callToolSender, "registry_id": float64(1),
		"account": callToolAccount, "role": "editor",
	}
}
