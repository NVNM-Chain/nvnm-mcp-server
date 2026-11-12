// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

const l1FeeTokenAddress = "0x20c0000000000000000000000000000000000000"

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFeeTokenFromEnv(t *testing.T) {
	const other = "0x1111111111111111111111111111111111111111"
	tests := []struct {
		name    string
		chainID int64
		env     map[string]string
		want    FeeToken
		wantErr error
	}{
		{
			name:    "L2 testnet charges fees in its native coin",
			chainID: testnetChainID,
			want:    FeeToken{},
		},
		{
			name:    "mainnet charges fees in its native coin",
			chainID: mainnetChainID,
			want:    FeeToken{},
		},
		{
			name:    "L1 devnet has a built-in fee token",
			chainID: l1DevnetChainID,
			want:    FeeToken{Symbol: "pathUSD", Decimals: 6, Address: l1FeeTokenAddress},
		},
		{
			name:    "each variable overrides one built-in field",
			chainID: l1DevnetChainID,
			env:     map[string]string{envFeeAssetSymbol: "nUSD"},
			want:    FeeToken{Symbol: "nUSD", Decimals: 6, Address: l1FeeTokenAddress},
		},
		{
			name:    "address and decimals override on a known chain",
			chainID: l1DevnetChainID,
			env:     map[string]string{envFeeAssetAddress: other, envFeeAssetDecimals: "18"},
			want:    FeeToken{Symbol: "pathUSD", Decimals: 18, Address: other},
		},
		{
			name:    "unknown chain with a full description",
			chainID: 58887,
			env: map[string]string{
				envFeeAssetAddress: other, envFeeAssetSymbol: " tUSD ", envFeeAssetDecimals: "0",
			},
			want: FeeToken{Symbol: "tUSD", Decimals: 0, Address: other},
		},
		{
			name:    "unknown chain without an address stays native",
			chainID: 58887,
			want:    FeeToken{},
		},
		{
			name:    "address alone on an unknown chain is incomplete",
			chainID: 58887,
			env:     map[string]string{envFeeAssetAddress: other},
			wantErr: ErrFeeTokenIncomplete,
		},
		{
			name:    "address without decimals on an unknown chain is incomplete",
			chainID: 58887,
			env:     map[string]string{envFeeAssetAddress: other, envFeeAssetSymbol: "tUSD"},
			wantErr: ErrFeeTokenIncomplete,
		},
		{
			name:    "symbol without an address on an unknown chain is incomplete",
			chainID: 58887,
			env:     map[string]string{envFeeAssetSymbol: "tUSD"},
			wantErr: ErrFeeTokenIncomplete,
		},
		{
			name:    "decimals without an address on a native-coin chain is incomplete",
			chainID: testnetChainID,
			env:     map[string]string{envFeeAssetDecimals: "6"},
			wantErr: ErrFeeTokenIncomplete,
		},
		{
			name:    "malformed address",
			chainID: l1DevnetChainID,
			env:     map[string]string{envFeeAssetAddress: "0x20c0"},
			wantErr: ErrFeeTokenAddressInvalid,
		},
		{
			// The EIP-55 vector from internal/evm/addrhex_test.go with its
			// casing broken. A bare hex parse accepts it; the balance read
			// does not, so startup must not either.
			name:    "mixed-case address with a wrong checksum",
			chainID: l1DevnetChainID,
			env: map[string]string{
				envFeeAssetAddress: "0x57eb2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- wrong checksum on purpose
			},
			wantErr: ErrFeeTokenAddressInvalid,
		},
		{
			name:    "mixed-case address with the right checksum",
			chainID: l1DevnetChainID,
			env: map[string]string{
				envFeeAssetAddress: "0x57EB2e9ee9345ce3dD4063E130D58EC79aba7207", // pragma: allowlist secret -- EIP-55 test vector
			},
			want: FeeToken{
				Symbol: "pathUSD", Decimals: 6,
				Address: "0x57EB2e9ee9345ce3dD4063E130D58EC79aba7207", // pragma: allowlist secret -- EIP-55 test vector
			},
		},
		{
			name:    "upper-case address carries no checksum",
			chainID: l1DevnetChainID,
			env: map[string]string{
				envFeeAssetAddress: "0x57EB2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- same vector, upper case
			},
			want: FeeToken{
				Symbol: "pathUSD", Decimals: 6,
				Address: "0x57EB2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- same vector, upper case
			},
		},
		{
			name:    "non-numeric decimals",
			chainID: l1DevnetChainID,
			env:     map[string]string{envFeeAssetDecimals: "six"},
			wantErr: ErrFeeTokenDecimalsInvalid,
		},
		{
			name:    "negative decimals",
			chainID: l1DevnetChainID,
			env:     map[string]string{envFeeAssetDecimals: "-1"},
			wantErr: ErrFeeTokenDecimalsInvalid,
		},
		{
			name:    "decimals above the cap",
			chainID: l1DevnetChainID,
			env:     map[string]string{envFeeAssetDecimals: "37"},
			wantErr: ErrFeeTokenDecimalsInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FeeTokenFromEnv(envMap(tt.env), tt.chainID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("FeeToken = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConfig_GasTokenAndNaming(t *testing.T) {
	t.Run("native coin takes the environment's name and 18 decimals", func(t *testing.T) {
		cfg := &Config{ChainEnvironment: EnvMainnet}
		if got, want := cfg.GasToken(), (FeeToken{Symbol: "wmmUSD", Decimals: 18}); got != want {
			t.Errorf("GasToken = %+v, want %+v", got, want)
		}
		if cfg.GasToken().IsContract() {
			t.Error("native coin reported as a token contract")
		}
		if got, want := cfg.TokenNaming(), (TokenNaming{Native: "mmUSD", Wrapped: "wmmUSD"}); got != want {
			t.Errorf("TokenNaming = %+v, want %+v", got, want)
		}
	})
	t.Run("fee token names both symbols and keeps its own scale", func(t *testing.T) {
		token := FeeToken{Symbol: "pathUSD", Decimals: 6, Address: l1FeeTokenAddress}
		cfg := &Config{ChainEnvironment: EnvTestnet, FeeToken: token}
		if got := cfg.GasToken(); got != token {
			t.Errorf("GasToken = %+v, want %+v", got, token)
		}
		if !cfg.GasToken().IsContract() {
			t.Error("fee token not reported as a token contract")
		}
		if got, want := cfg.TokenNaming(), (TokenNaming{Native: "pathUSD", Wrapped: "pathUSD"}); got != want {
			t.Errorf("TokenNaming = %+v, want %+v", got, want)
		}
	})
}

// TestLoad_L1Devnet proves chain 787222 boots with no chain-specific
// variables beyond the RPC URL and chain ID: the environment is inferred
// and the fee token is the built-in one.
func TestLoad_L1Devnet(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("NVNM_CHAIN_ID", "787222")
	t.Setenv("NVNM_CHAIN_ENVIRONMENT", "")
	t.Setenv("NVNM_FAUCET_URL", "https://faucet.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainEnvironment != EnvTestnet {
		t.Errorf("ChainEnvironment = %q, want testnet (inferred from 787222)", cfg.ChainEnvironment)
	}
	want := FeeToken{Symbol: "pathUSD", Decimals: 6, Address: l1FeeTokenAddress}
	if cfg.FeeToken != want {
		t.Errorf("FeeToken = %+v, want %+v", cfg.FeeToken, want)
	}
	if cfg.FaucetURL != "https://faucet.example.com" {
		t.Errorf("FaucetURL = %q", cfg.FaucetURL)
	}
}

func TestLoad_FeeTokenMisconfigurationFailsFast(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("NVNM_FEE_TOKEN_ADDRESS", "0x1111111111111111111111111111111111111111")

	if _, err := Load(); !errors.Is(err, ErrFeeTokenIncomplete) {
		t.Fatalf("Load error = %v, want ErrFeeTokenIncomplete", err)
	}
}

func TestLoad_NativeCoinChainHasNoFeeToken(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FeeToken.IsContract() {
		t.Errorf("FeeToken = %+v, want the zero value on a native-coin chain", cfg.FeeToken)
	}
}

// TestValidFeeTokenAddress_MatchesBalanceReadParser pins the startup check
// to the parser the balance read uses (evm.ParseAddress). If the two ever
// disagree, an address either passes startup and then fails every balance
// call, or is refused at startup though it would have worked.
func TestValidFeeTokenAddress_MatchesBalanceReadParser(t *testing.T) {
	vectors := []string{
		l1FeeTokenAddress,
		"0x57EB2e9ee9345ce3dD4063E130D58EC79aba7207", // pragma: allowlist secret -- EIP-55 test vector
		"0x57eb2e9ee9345ce3dd4063e130d58ec79aba7207", // pragma: allowlist secret -- same vector, lower case
		"0x57EB2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- same vector, upper case
		"57EB2e9ee9345ce3dD4063E130D58EC79aba7207",   // pragma: allowlist secret -- same vector, no 0x
		"0X57eb2e9ee9345ce3dd4063e130d58ec79aba7207", // pragma: allowlist secret -- same vector, 0X prefix
		"0x57eb2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- wrong checksum on purpose
		"0x20C0000000000000000000000000000000000000",
		"0x1234",
		"not-an-address",
		"",
	}
	for _, v := range vectors {
		_, err := evm.ParseAddress(v)
		if got, want := validFeeTokenAddress(v), err == nil; got != want {
			t.Errorf("validFeeTokenAddress(%q) = %v, but evm.ParseAddress accepts = %v", v, got, want)
		}
	}
}
