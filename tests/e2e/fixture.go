// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StaleHeadAge is how old the latest block may be before the suite
// reports a halted chain rather than a test failure.
const StaleHeadAge = 15 * time.Minute

// SetupDiscovery records chain identity, whether writes are advertised,
// and whether the signing wallet can pay for the hot path.
func SetupDiscovery(t *testing.T, f *Flow) {
	t.Helper()

	res, err := f.Session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	t.Logf("server advertises %d tools: %v", len(names), names)

	f.WriteToolsAvailable = true
	for _, required := range []string{
		"anchor_prepare_add_registry",
		"anchor_prepare_add_record",
		"evm_send_raw_transaction",
	} {
		if !Contains(names, required) {
			t.Logf("server does not advertise %q; the write path is unavailable", required)
			f.WriteToolsAvailable = false
		}
	}
	f.LifecycleToolsAvailable = Contains(names, "anchor_prepare_update_record_status")

	var overview OverviewResponse
	f.CallOK(t, "nvnm_overview", map[string]any{}, &overview)
	if overview.ChainID == 0 {
		t.Fatal("nvnm_overview reports chain_id 0; nothing downstream can validate a prepared transaction")
	}
	if overview.AnchorPrecompile == "" {
		t.Fatal("nvnm_overview reports no anchor_precompile; nothing downstream can validate a destination")
	}
	if !strings.HasPrefix(strings.ToLower(overview.AnchorPrecompile), "0x") {
		t.Errorf("anchor_precompile = %q, want 0x-prefixed address", overview.AnchorPrecompile)
	}
	if overview.ChainName == "" || overview.TokenNative == "" {
		t.Errorf("overview missing chain_name or token_native: name=%q token=%q",
			overview.ChainName, overview.TokenNative)
	}
	f.ChainID = overview.ChainID
	f.AnchorAddress = overview.AnchorPrecompile
	if strings.EqualFold(overview.ChainEnvironment, "mainnet") {
		t.Log("chain_environment=mainnet: write path skipped (read-only smoke)")
		f.WriteToolsAvailable = false
		f.MainnetReadOnly = true
	}
	t.Logf("chain=%s env=%s chain_id=%d precompile=%s",
		overview.ChainName, overview.ChainEnvironment, f.ChainID, f.AnchorAddress)

	var wallet WalletStatusResponse
	f.CallOK(t, "wallet_status", map[string]any{"address": f.Address}, &wallet)
	if wallet.Address != "" && !strings.EqualFold(wallet.Address, f.Address) {
		t.Errorf("wallet_status.address = %s, want %s", wallet.Address, f.Address)
	}
	if wallet.BalanceWei == "" {
		t.Error("wallet_status.balance_wei empty")
	}
	f.WalletFunded = wallet.Status != "unfunded"
	t.Logf("wallet %s: status=%s nonce=%d balance=%s",
		f.Address, wallet.Status, wallet.Nonce, wallet.BalanceHuman)

	setupOnboarding(t, f)
}

// setupOnboarding is the read-only start of the operator journey: wizard,
// chain identity, balance, precompile info, and the two setup-verify tools.
// Grant/revoke stay out of this path (admin-only MCP role; no role-read tool).
func setupOnboarding(t *testing.T, f *Flow) {
	t.Helper()

	var wizard WizardResponse
	f.CallOK(t, "nvnm_setup_wizard", map[string]any{"address": f.Address}, &wizard)
	switch wizard.State {
	case "funded_unused", "funded_active":
	case "unfunded":
		t.Log("nvnm_setup_wizard reports unfunded; write path will skip")
	default:
		t.Errorf("nvnm_setup_wizard state = %q, want funded_unused, funded_active, or unfunded", wizard.State)
	}
	if wizard.Wallet == nil {
		t.Fatal("nvnm_setup_wizard wallet snapshot missing")
	} else if wizard.Wallet.Address != "" && !strings.EqualFold(wizard.Wallet.Address, f.Address) {
		t.Errorf("wizard wallet.address = %s, want %s", wizard.Wallet.Address, f.Address)
	}

	var chain ChainIDResponse
	f.CallOK(t, "evm_get_chain_id", map[string]any{}, &chain)
	if chain.ChainID != f.ChainID {
		t.Errorf("evm_get_chain_id = %d, want %d from nvnm_overview", chain.ChainID, f.ChainID)
	}
	if chain.LatestBlockNumber == 0 {
		t.Error("evm_get_chain_id.latest_block_number is 0")
	}

	var bal BalanceResponse
	f.CallOK(t, "evm_get_balance", map[string]any{"address": f.Address}, &bal)
	if bal.Address != "" && !strings.EqualFold(bal.Address, f.Address) {
		t.Errorf("evm_get_balance.address = %s, want %s", bal.Address, f.Address)
	}
	if _, ok := new(big.Int).SetString(bal.Wei, 10); !ok {
		t.Errorf("evm_get_balance.wei = %q, want a decimal integer", bal.Wei)
	}

	var info AnchorInfoResponse
	f.CallOK(t, "anchor_info", map[string]any{}, &info)
	if !strings.EqualFold(info.Address, f.AnchorAddress) {
		t.Errorf("anchor_info.address = %s, want %s", info.Address, f.AnchorAddress)
	}
	if info.ChainID != f.ChainID {
		t.Errorf("anchor_info.chain_id = %d, want %d", info.ChainID, f.ChainID)
	}
	if !info.ABILoaded {
		t.Error("anchor_info.abi_loaded = false")
	}

	var mismatch VerifyHashResponse
	f.CallOK(t, "nvnm_setup_verify_hash", map[string]any{
		"address": f.Address,
		"hash":    "0x" + strings.Repeat("00", 32),
	}, &mismatch)
	if mismatch.OK {
		t.Fatal("nvnm_setup_verify_hash accepted a zero digest")
	}
	if mismatch.Expected == "" || mismatch.Challenge == "" {
		t.Fatal("nvnm_setup_verify_hash mismatch did not echo expected/challenge")
	}

	var hashOK VerifyHashResponse
	f.CallOK(t, "nvnm_setup_verify_hash", map[string]any{
		"address": f.Address,
		"hash":    mismatch.Expected,
	}, &hashOK)
	if !hashOK.OK {
		t.Errorf("nvnm_setup_verify_hash ok=false expected=%s got=%s", hashOK.Expected, hashOK.Got)
	}

	sig, err := f.Key.SignMessage(context.Background(), []byte(hashOK.Challenge))
	if err != nil {
		t.Fatalf("sign setup challenge: %v", err)
	}
	var sigOut VerifySignatureResponse
	f.CallOK(t, "nvnm_setup_verify_signature", map[string]any{
		"address":   f.Address,
		"signature": sig.String(),
	}, &sigOut)
	if !sigOut.OK {
		t.Errorf("nvnm_setup_verify_signature ok=false recovered=%s", sigOut.Recovered)
	}
	if sigOut.Recovered != "" && !strings.EqualFold(sigOut.Recovered, f.Address) {
		t.Errorf("recovered_address = %s, want %s", sigOut.Recovered, f.Address)
	}
}

// PreflightChainLive fails with one clear sentence when the chain is not
// advancing, so a halt is never debugged as a nonce or timeout error
// deep inside a write flow.
func PreflightChainLive(t *testing.T, f *Flow) {
	t.Helper()

	var blk BlockResponse
	f.CallOK(t, "evm_get_block", map[string]any{}, &blk)
	if blk.TimestampUnix == 0 {
		t.Fatal("chain liveness: latest block has timestamp 0")
	}
	if blk.Number == 0 {
		t.Error("evm_get_block.number is 0")
	}
	if !strings.HasPrefix(blk.Hash, "0x") {
		t.Errorf("evm_get_block.hash = %q, want 0x-prefixed", blk.Hash)
	}
	age := time.Since(time.Unix(int64(blk.TimestampUnix), 0)) //nolint:gosec // unix seconds fit int64
	if age > StaleHeadAge {
		t.Fatalf("chain appears halted: latest block %d is %s old (timestamp %d). "+
			"This is a chain outage, not a server or test failure.",
			blk.Number, age.Truncate(time.Second), blk.TimestampUnix)
	}
	t.Logf("chain live: head=%d age=%s", blk.Number, age.Truncate(time.Second))
}
