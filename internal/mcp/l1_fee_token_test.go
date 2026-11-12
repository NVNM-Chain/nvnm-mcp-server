// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

const (
	l1TestFeeToken = "0x20c0000000000000000000000000000000000000"
	// l1NativePlaceholder is what eth_getBalance answered for every
	// address on chain 787222 (2026-11-09), including the zero address.
	l1NativePlaceholder = "4242424242424242424242424242424242424242424242424242424242424242424242424242"
)

// l1ServerConfig is testServerConfig for the L1 devnet: fees charged in a
// 6-decimal token contract, no native coin.
func l1ServerConfig() *config.Config {
	cfg := testServerConfig(false)
	cfg.ChainID = 787222
	cfg.FeeToken = config.FeeToken{Symbol: "pathUSD", Decimals: 6, Address: l1TestFeeToken}
	return cfg
}

// l1Mock is a node whose native balance is the placeholder and whose fee
// token balanceOf returns tokenUnits. A handler that still reads the native
// balance reports every wallet as funded.
func l1Mock(tokenUnits int64, nonce uint64) *mockEVM {
	out := make([]byte, 32)
	big.NewInt(tokenUnits).FillBytes(out)
	return &mockEVM{
		balance:    &evm.NormalizedBalance{Address: testAddr, Wei: l1NativePlaceholder, Ether: "4242.42"},
		callResult: out,
		nonce:      nonce,
	}
}

func assertBalanceOfCall(t *testing.T, m *mockEVM) {
	t.Helper()
	if m.lastCall.To == nil || *m.lastCall.To != defitypes.MustAddressFromHex(l1TestFeeToken) {
		t.Fatalf("balance read target = %v, want the fee token contract", m.lastCall.To)
	}
	if len(m.lastCall.Input) != 36 {
		t.Fatalf("balance read calldata = %x, want a 36-byte balanceOf call", m.lastCall.Input)
	}
	if got := m.lastCall.Input[:4]; got[0] != 0x70 || got[1] != 0xa0 || got[2] != 0x82 || got[3] != 0x31 {
		t.Errorf("selector = %x, want 70a08231 (balanceOf)", got)
	}
}

// TestWalletStatus_FeeTokenChain proves the funded state comes from the fee
// token, not from eth_getBalance, on a chain with no native coin.
func TestWalletStatus_FeeTokenChain(t *testing.T) {
	cases := []struct {
		name       string
		tokenUnits int64
		nonce      uint64
		wantStatus string
		wantWei    string
		wantHuman  string
	}{
		{"placeholder native balance does not fund a wallet", 0, 0, WalletStatusUnfunded, "0", "0.000000 pathUSD"},
		{"faucet amount, never used", 100_000_000, 0, WalletStatusFundedUnused, "100000000", "100.000000 pathUSD"},
		{"funded and active", 99_946_588, 15, WalletStatusFundedActive, "99946588", "99.946588 pathUSD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := l1Mock(tc.tokenUnits, tc.nonce)
			_, out, err := makeWalletStatusHandler(m, l1ServerConfig())(ctx, nil, walletStatusInput{Address: testAddr})
			if err != nil {
				t.Fatalf("wallet_status: %v", err)
			}
			if out.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", out.Status, tc.wantStatus)
			}
			if out.BalanceWei != tc.wantWei || out.BalanceHuman != tc.wantHuman {
				t.Errorf("balance = (%q, %q), want (%q, %q)", out.BalanceWei, out.BalanceHuman, tc.wantWei, tc.wantHuman)
			}
			if out.BalanceDecimals != 6 {
				t.Errorf("balance_decimals = %d, want 6", out.BalanceDecimals)
			}
			if out.FeeTokenAddress != l1TestFeeToken {
				t.Errorf("fee_token_address = %q, want %q", out.FeeTokenAddress, l1TestFeeToken)
			}
			if out.TokenNative != "pathUSD" || out.TokenWrapped != "pathUSD" {
				t.Errorf("token names = (%q, %q), want pathUSD for both", out.TokenNative, out.TokenWrapped)
			}
			assertBalanceOfCall(t, m)
		})
	}
}

// TestWalletStatus_NativeCoinChainUnchanged pins the L2 shape: the native
// balance, 18 decimals, and no fee token address.
func TestWalletStatus_NativeCoinChainUnchanged(t *testing.T) {
	m := &mockEVM{
		balance: &evm.NormalizedBalance{Address: testAddr, Wei: "1000000000000000000", Ether: "1.000000000000000000"},
	}
	_, out, err := makeWalletStatusHandler(m, testServerConfig(false))(ctx, nil, walletStatusInput{Address: testAddr})
	if err != nil {
		t.Fatalf("wallet_status: %v", err)
	}
	if out.Status != WalletStatusFundedUnused || out.BalanceHuman != "1.000000000000000000 wmantraUSD" {
		t.Errorf("status/balance = (%q, %q)", out.Status, out.BalanceHuman)
	}
	if out.BalanceDecimals != 18 || out.FeeTokenAddress != "" {
		t.Errorf("decimals/fee token = (%d, %q), want (18, empty)", out.BalanceDecimals, out.FeeTokenAddress)
	}
	if m.lastCall.To != nil {
		t.Errorf("native-coin chain made a contract call to %v", m.lastCall.To)
	}
}

func TestWalletStatus_FeeTokenReadFails(t *testing.T) {
	m := l1Mock(1, 0)
	m.callResult = nil // a token that is not deployed answers with no bytes
	_, _, err := makeWalletStatusHandler(m, l1ServerConfig())(ctx, nil, walletStatusInput{Address: testAddr})
	if !errors.Is(err, apperrors.ErrNodeResponseDecode) {
		t.Fatalf("err = %v, want ErrNodeResponseDecode; an unreadable balance is not zero", err)
	}
}

func TestSetupWizard_FeeTokenChain(t *testing.T) {
	t.Run("unfunded names the fee token and the faucet", func(t *testing.T) {
		cfg := l1ServerConfig()
		cfg.FaucetURL = "https://faucet.example"
		_, out, err := makeSetupWizardHandler(l1Mock(0, 0), cfg)(ctx, nil, setupWizardInput{Address: testAddr})
		if err != nil {
			t.Fatalf("wizard: %v", err)
		}
		if out.State != WizardStateUnfunded {
			t.Fatalf("state = %q, want unfunded (the native placeholder is not a balance)", out.State)
		}
		for _, want := range []string{"pathUSD", "no native coin", "Faucet: https://faucet.example"} {
			if !strings.Contains(out.Message, want) {
				t.Errorf("message missing %q: %q", want, out.Message)
			}
		}
		if strings.Contains(out.Message, "bridge") {
			t.Errorf("message points at a bridge this deployment does not configure: %q", out.Message)
		}
		if out.FaucetURL != cfg.FaucetURL || out.BridgeURL != "" {
			t.Errorf("urls = (faucet %q, bridge %q)", out.FaucetURL, out.BridgeURL)
		}
		if out.Wallet == nil || out.Wallet.BalanceDecimals != 6 || out.Wallet.FeeTokenAddress != l1TestFeeToken {
			t.Errorf("wallet snapshot = %+v", out.Wallet)
		}
	})
	t.Run("funded reports the token amount", func(t *testing.T) {
		_, out, err := makeSetupWizardHandler(l1Mock(100_000_000, 0), l1ServerConfig())(
			ctx, nil, setupWizardInput{Address: testAddr})
		if err != nil {
			t.Fatalf("wizard: %v", err)
		}
		if out.State != WizardStateFundedUnused {
			t.Fatalf("state = %q, want funded_unused", out.State)
		}
		if !strings.Contains(out.Message, "100.000000 pathUSD") {
			t.Errorf("message = %q, want the token amount", out.Message)
		}
	})
	t.Run("needs_wallet points at the configured funding field", func(t *testing.T) {
		cfg := l1ServerConfig()
		cfg.FaucetURL = "https://faucet.example"
		_, out, err := makeSetupWizardHandler(&mockEVM{}, cfg)(ctx, nil, setupWizardInput{})
		if err != nil {
			t.Fatalf("wizard: %v", err)
		}
		if !strings.Contains(out.Message, "fund it with pathUSD (see `faucet_url`)") {
			t.Errorf("message = %q", out.Message)
		}
		if out.FaucetURL != cfg.FaucetURL {
			t.Errorf("faucet_url = %q", out.FaucetURL)
		}
	})
}

// TestSetupWizard_NativeCoinUnfundedMessageUnchanged pins the L2 wording.
func TestSetupWizard_NativeCoinUnfundedMessageUnchanged(t *testing.T) {
	cfg := testServerConfig(false)
	cfg.BridgeURL = "https://bridge.example"
	m := &mockEVM{balance: &evm.NormalizedBalance{Address: testAddr, Wei: "0", Ether: "0"}}
	_, out, err := makeSetupWizardHandler(m, cfg)(ctx, nil, setupWizardInput{Address: testAddr})
	if err != nil {
		t.Fatalf("wizard: %v", err)
	}
	want := "Wallet has no balance. Fund it with wmantraUSD via the bridge, then re-call this wizard." +
		" Bridge: https://bridge.example"
	if out.Message != want {
		t.Errorf("message = %q, want %q", out.Message, want)
	}
}

func TestFundingHint(t *testing.T) {
	pathUSD := config.FeeToken{Symbol: "pathUSD", Decimals: 6, Address: l1TestFeeToken}
	tests := []struct {
		name           string
		faucet, bridge string
		feeToken       config.FeeToken
		want           string
	}{
		{"native coin, nothing configured: the bridge field, as before", "", "", config.FeeToken{}, "see `bridge_url`"},
		{"native coin, bridge", "", "https://b", config.FeeToken{}, "see `bridge_url`"},
		{"native coin, faucet", "https://f", "", config.FeeToken{}, "see `faucet_url`"},
		{"native coin, both", "https://f", "https://b", config.FeeToken{}, "see `faucet_url` or `bridge_url`"},
		{"fee token, faucet", "https://f", "", pathUSD, "see `faucet_url`"},
		{"fee token, bridge", "", "https://b", pathUSD, "see `bridge_url`"},
		{"fee token, both", "https://f", "https://b", pathUSD, "see `faucet_url` or `bridge_url`"},
		{
			"fee token, nothing configured: no field to point at", "", "", pathUSD,
			"this deployment lists no funding URL; ask its operator where to get pathUSD",
		},
	}
	for _, tt := range tests {
		cfg := &config.Config{FaucetURL: tt.faucet, BridgeURL: tt.bridge, FeeToken: tt.feeToken}
		if got := fundingHint(cfg); got != tt.want {
			t.Errorf("%s: fundingHint = %q, want %q", tt.name, got, tt.want)
		}
	}
	both := &config.Config{FaucetURL: "https://f", BridgeURL: "https://b"}
	if got, want := fundingURLs(both), " Faucet: https://f Bridge: https://b"; got != want {
		t.Errorf("fundingURLs = %q, want %q", got, want)
	}
	if got := fundingURLs(&config.Config{}); got != "" {
		t.Errorf("fundingURLs with nothing configured = %q, want empty", got)
	}
}

// TestFeeTokenChain_NoFundingURLConfigured is the minimal L1 deployment:
// RPC URL and chain ID only. No response carries bridge_url or faucet_url,
// so no text may send the reader to either field.
func TestFeeTokenChain_NoFundingURLConfigured(t *testing.T) {
	cfg := l1ServerConfig()
	const operator = "ask its operator where to get pathUSD"

	_, wizard, err := makeSetupWizardHandler(&mockEVM{}, cfg)(ctx, nil, setupWizardInput{})
	if err != nil {
		t.Fatalf("wizard needs_wallet: %v", err)
	}
	_, unfunded, err := makeSetupWizardHandler(l1Mock(0, 0), cfg)(ctx, nil, setupWizardInput{Address: testAddr})
	if err != nil {
		t.Fatalf("wizard unfunded: %v", err)
	}
	_, overview, err := makeOverviewHandler(cfg)(ctx, nil, overviewInput{})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if wizard.BridgeURL != "" || wizard.FaucetURL != "" || unfunded.BridgeURL != "" || overview.BridgeURL != "" {
		t.Fatal("a funding URL is set; this test is about the deployment that configures none")
	}

	texts := map[string]string{
		"wizard needs_wallet message":   wizard.Message,
		"wizard unfunded message":       unfunded.Message,
		"overview funding prerequisite": overview.Prereqs[fundingPrereqIndex],
	}
	for name, text := range texts {
		if strings.Contains(text, "bridge_url") || strings.Contains(text, "faucet_url") {
			t.Errorf("%s points at a field this response does not carry: %q", name, text)
		}
		if !strings.Contains(text, operator) {
			t.Errorf("%s must send the reader to the operator: %q", name, text)
		}
	}
}

func TestGetBalance_FeeTokenChain(t *testing.T) {
	m := l1Mock(2466, 0)
	_, out, err := makeGetBalanceHandler(m, l1ServerConfig())(ctx, nil, getBalanceInput{Address: testAddr})
	if err != nil {
		t.Fatalf("evm_get_balance: %v", err)
	}
	if out.Wei != "2466" || out.Ether != "0.002466" || out.BalanceHuman != "0.002466 pathUSD" {
		t.Errorf("balance = (%q, %q, %q)", out.Wei, out.Ether, out.BalanceHuman)
	}
	if out.TokenWrapped != "pathUSD" || out.BalanceDecimals != 6 || out.FeeTokenAddress != l1TestFeeToken {
		t.Errorf("token = (%q, %d, %q)", out.TokenWrapped, out.BalanceDecimals, out.FeeTokenAddress)
	}
	assertBalanceOfCall(t, m)
}

// TestGetBalance_FeeTokenChain_MissingBlock proves a historical balance at
// a block the node does not have keeps its reason on the token path, the
// way the native path does (chain 787222: "block not found: 0x3b9ac9ff").
func TestGetBalance_FeeTokenChain_MissingBlock(t *testing.T) {
	m := l1Mock(1, 0)
	m.returnErr = errors.New("RPC error: -32001 block not found: 0x3b9ac9ff")
	_, _, err := makeGetBalanceHandler(m, l1ServerConfig())(ctx, nil,
		getBalanceInput{Address: testAddr, BlockNum: blockNumArg(999_999_999)})
	if !errors.Is(err, apperrors.ErrBlockNotFound) {
		t.Fatalf("err = %v, want ErrBlockNotFound", err)
	}
	if got := apperrors.SafeForClient(err).Error(); got == "upstream operation failed" ||
		strings.Contains(got, "0x3b9ac9ff") {
		t.Errorf("client sees %q", got)
	}
}

func TestGetBalance_NativeCoinChainReportsScale(t *testing.T) {
	m := &mockEVM{balance: &evm.NormalizedBalance{Address: testAddr, Wei: "5", Ether: "0.000000000000000005"}}
	_, out, err := makeGetBalanceHandler(m, testServerConfig(false))(ctx, nil, getBalanceInput{Address: testAddr})
	if err != nil {
		t.Fatalf("evm_get_balance: %v", err)
	}
	if out.BalanceDecimals != 18 || out.FeeTokenAddress != "" || out.TokenWrapped != "wmantraUSD" {
		t.Errorf("native shape = (%d, %q, %q)", out.BalanceDecimals, out.FeeTokenAddress, out.TokenWrapped)
	}
}

// TestOverview_FeeTokenChain proves the lobby text does not describe the L1
// as an L2 on MANTRA, and says up front that there is no native coin.
func TestOverview_FeeTokenChain(t *testing.T) {
	cfg := l1ServerConfig()
	cfg.FaucetURL = "https://faucet.example"
	_, out, err := makeOverviewHandler(cfg)(ctx, nil, overviewInput{})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if strings.Contains(out.WhatIsNVNMChain, "L2") || strings.Contains(out.WhatIsNVNMChain, "MANTRA") {
		t.Errorf("L1 described as an L2 on MANTRA: %q", out.WhatIsNVNMChain)
	}
	for _, want := range []string{"EVM L1", "no native coin", "pathUSD", "SHA-256"} {
		if !strings.Contains(out.WhatIsNVNMChain, want) {
			t.Errorf("what_is_nvnm_chain missing %q: %q", want, out.WhatIsNVNMChain)
		}
	}
	if out.FeeTokenAddress != l1TestFeeToken || out.FaucetURL != cfg.FaucetURL {
		t.Errorf("fee token / faucet = (%q, %q)", out.FeeTokenAddress, out.FaucetURL)
	}
	if out.TokenNative != "pathUSD" || out.TokenWrapped != "pathUSD" {
		t.Errorf("token names = (%q, %q)", out.TokenNative, out.TokenWrapped)
	}
	funding := out.Prereqs[fundingPrereqIndex]
	if !strings.Contains(funding, "pathUSD") || !strings.Contains(funding, "`faucet_url`") {
		t.Errorf("funding prerequisite = %q", funding)
	}
	if basePrereqs[fundingPrereqIndex] == funding {
		t.Error("funding prerequisite not specialised for the fee token")
	}
}

// TestInitializeInstructions_PerChain pins the session instructions every
// client receives at initialize: the L1 must not be introduced as an L2 on
// MANTRA Chain, and the L2 text is byte-for-byte what it was.
func TestInitializeInstructions_PerChain(t *testing.T) {
	l2 := initializeInstructionsFor(testServerConfig(false))
	if l2 != initializeInstructions || !strings.Contains(l2, "an EVM L2 on MANTRA Chain used as a neutral notary") {
		t.Errorf("L2 instructions changed: %q", l2)
	}
	l1 := initializeInstructionsFor(l1ServerConfig())
	if strings.Contains(l1, "L2") || strings.Contains(l1, "MANTRA") {
		t.Errorf("L1 instructions describe an L2 on MANTRA: %q", l1)
	}
	if !strings.Contains(l1, "an EVM L1 used as a neutral notary") {
		t.Errorf("L1 instructions = %q", l1)
	}
	if rest := strings.TrimPrefix(l1, "NVNM Chain MCP server -- typed access to NVNM Chain, an EVM L1 used as a "); rest != initializeInstructionsBody {
		t.Errorf("L1 instructions body diverged from the shared text")
	}
}

func TestOverview_NativeCoinChainUnchanged(t *testing.T) {
	_, out, err := makeOverviewHandler(testServerConfig(false))(ctx, nil, overviewInput{})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.WhatIsNVNMChain != whatIsNVNMChainText || !strings.Contains(out.WhatIsNVNMChain, "EVM L2 on MANTRA Chain") {
		t.Errorf("L2 description changed: %q", out.WhatIsNVNMChain)
	}
	if out.FeeTokenAddress != "" || out.FaucetURL != "" {
		t.Errorf("fee token / faucet = (%q, %q), want empty", out.FeeTokenAddress, out.FaucetURL)
	}
	if out.Prereqs[fundingPrereqIndex] != basePrereqs[fundingPrereqIndex] {
		t.Errorf("funding prerequisite changed on a native-coin chain: %q", out.Prereqs[fundingPrereqIndex])
	}
}
