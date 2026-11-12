// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

const devnetFeeTokenAddress = "0x20c0000000000000000000000000000000000000"

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFeeTokenFromEnv(t *testing.T) {
	const other = "0x1111111111111111111111111111111111111111"
	builtIn := FeeToken{Symbol: "pathUSD", Decimals: 6, Address: devnetFeeTokenAddress}
	tests := []struct {
		name    string
		chainID int64
		env     map[string]string
		want    FeeToken
		wantErr error
	}{
		{
			name:    "the devnet has a built-in fee token",
			chainID: devnetChainID,
			want:    builtIn,
		},
		{
			name:    "each variable overrides one built-in field",
			chainID: devnetChainID,
			env:     map[string]string{envFeeAssetSymbol: "nUSD"},
			want:    FeeToken{Symbol: "nUSD", Decimals: 6, Address: devnetFeeTokenAddress},
		},
		{
			name:    "address and decimals override on a known chain",
			chainID: devnetChainID,
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
			name:    "unknown chain with no fee token",
			chainID: 58887,
			wantErr: ErrFeeTokenRequired,
		},
		{
			name:    "unknown chain with the address alone",
			chainID: 58887,
			env:     map[string]string{envFeeAssetAddress: other},
			wantErr: ErrFeeTokenRequired,
		},
		{
			name:    "unknown chain without decimals",
			chainID: 58887,
			env:     map[string]string{envFeeAssetAddress: other, envFeeAssetSymbol: "tUSD"},
			wantErr: ErrFeeTokenRequired,
		},
		{
			name:    "unknown chain without a symbol",
			chainID: 58887,
			env:     map[string]string{envFeeAssetAddress: other, envFeeAssetDecimals: "6"},
			wantErr: ErrFeeTokenRequired,
		},
		{
			name:    "unknown chain without an address",
			chainID: 58887,
			env:     map[string]string{envFeeAssetSymbol: "tUSD", envFeeAssetDecimals: "6"},
			wantErr: ErrFeeTokenRequired,
		},
		{
			name:    "a retired chain ID has no built-in fee token",
			chainID: 787111,
			wantErr: ErrFeeTokenRequired,
		},
		{
			name:    "malformed address",
			chainID: devnetChainID,
			env:     map[string]string{envFeeAssetAddress: "0x20c0"},
			wantErr: ErrFeeTokenAddressInvalid,
		},
		{
			// The EIP-55 vector from internal/evm/addrhex_test.go with its
			// casing broken. A bare hex parse accepts it; the balance read
			// does not, so startup must not either.
			name:    "mixed-case address with a wrong checksum",
			chainID: devnetChainID,
			env: map[string]string{
				envFeeAssetAddress: "0x57eb2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- wrong checksum on purpose
			},
			wantErr: ErrFeeTokenAddressInvalid,
		},
		{
			name:    "mixed-case address with the right checksum",
			chainID: devnetChainID,
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
			chainID: devnetChainID,
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
			chainID: devnetChainID,
			env:     map[string]string{envFeeAssetDecimals: "six"},
			wantErr: ErrFeeTokenDecimalsInvalid,
		},
		{
			name:    "negative decimals",
			chainID: devnetChainID,
			env:     map[string]string{envFeeAssetDecimals: "-1"},
			wantErr: ErrFeeTokenDecimalsInvalid,
		},
		{
			name:    "decimals above the cap",
			chainID: devnetChainID,
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

// TestLoad_Devnet proves chain 787222 boots with no chain-specific
// variables beyond the RPC URL and chain ID: the environment is inferred
// and the fee token is the built-in one.
func TestLoad_Devnet(t *testing.T) {
	clearEnv(t)
	t.Setenv("NVNM_EVM_RPC_URL", "https://rpc.nvnm.canary.mantrachain.dev")
	t.Setenv("NVNM_CHAIN_ID", "787222")
	t.Setenv("NVNM_FAUCET_URL", "https://faucet.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainEnvironment != EnvTestnet {
		t.Errorf("ChainEnvironment = %q, want testnet (inferred from 787222)", cfg.ChainEnvironment)
	}
	want := FeeToken{Symbol: "pathUSD", Decimals: 6, Address: devnetFeeTokenAddress}
	if cfg.FeeToken != want {
		t.Errorf("FeeToken = %+v, want %+v", cfg.FeeToken, want)
	}
	if cfg.FaucetURL != "https://faucet.example.com" {
		t.Errorf("FaucetURL = %q", cfg.FaucetURL)
	}
}

// TestLoad_UnknownChainRequiresFeeToken proves the server refuses to
// start on a chain whose fee token it was not told, in full: there is no
// other balance it could report.
func TestLoad_UnknownChainRequiresFeeToken(t *testing.T) {
	for name, unset := range map[string]string{
		"no address":  "NVNM_FEE_TOKEN_ADDRESS",
		"no symbol":   "NVNM_FEE_TOKEN_SYMBOL",
		"no decimals": "NVNM_FEE_TOKEN_DECIMALS",
	} {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			setMinimalEnv(t)
			t.Setenv(unset, "")
			if _, err := Load(); !errors.Is(err, ErrFeeTokenRequired) {
				t.Fatalf("Load error = %v, want ErrFeeTokenRequired", err)
			}
		})
	}

	clearEnv(t)
	setMinimalEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with a full description: %v", err)
	}
	want := FeeToken{Symbol: "tUSD", Decimals: 6, Address: "0x1111111111111111111111111111111111111111"}
	if cfg.FeeToken != want {
		t.Errorf("FeeToken = %+v, want %+v", cfg.FeeToken, want)
	}
}

// TestValidFeeTokenAddress_MatchesBalanceReadParser pins the startup check
// to the parser the balance read uses (evm.ParseAddress). If the two ever
// disagree, an address either passes startup and then fails every balance
// call, or is refused at startup though it would have worked.
func TestValidFeeTokenAddress_MatchesBalanceReadParser(t *testing.T) {
	vectors := []string{
		devnetFeeTokenAddress,
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
