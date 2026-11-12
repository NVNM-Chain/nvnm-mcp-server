// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

// ChainEnvironment identifies which NVNM Chain environment the server is
// configured against. It is a label surfaced to callers; it does not
// change how the server talks to the chain.
type ChainEnvironment string

const (
	// EnvTestnet is a test network.
	EnvTestnet ChainEnvironment = "testnet"
	// EnvMainnet is the production network.
	EnvMainnet ChainEnvironment = "mainnet"
)

// IsValid reports whether e is one of the two recognized environments.
func (e ChainEnvironment) IsValid() bool {
	return e == EnvTestnet || e == EnvMainnet
}

// String satisfies the Stringer interface.
func (e ChainEnvironment) String() string { return string(e) }

// devnetChainID is nvnm-tempo-devnet-1, the one chain the server knows
// without being told: its environment is inferred and its fee token is
// built in. Any other chain sets NVNM_CHAIN_ENVIRONMENT and the
// NVNM_FEE_TOKEN_* variables.
const devnetChainID int64 = 787222

// InferEnvironmentFromChainID returns the environment a chain ID maps
// to, or an empty value if the chain ID is unrecognized. An empty
// return is the signal that the operator must set
// NVNM_CHAIN_ENVIRONMENT.
func InferEnvironmentFromChainID(chainID int64) ChainEnvironment {
	if chainID == devnetChainID {
		return EnvTestnet
	}
	return ""
}

// maxFeeTokenDecimals bounds NVNM_FEE_TOKEN_DECIMALS. ERC-20 decimals is a
// uint8; nothing in use exceeds 18, and 36 leaves room without letting a
// typo produce a balance string hundreds of digits long.
const maxFeeTokenDecimals = 36

// FeeToken is the asset a wallet pays transaction fees in.
//
// NVNM Chain has no native coin. Fees are charged in a token contract, and
// eth_getBalance answers the same placeholder for every address (observed
// 2026-11-09 on chain 787222), so the only meaningful balance is the
// token's balanceOf.
type FeeToken struct {
	// Symbol is the customer-facing name, e.g. pathUSD.
	Symbol string
	// Decimals is the token's scale: the balance integer divided by
	// 10^Decimals is the human amount.
	Decimals int
	// Address is the token contract.
	Address string
}

// knownFeeTokens is the built-in fee token per known chain. Observed on a
// mined addRegistry receipt on chain 787222 (2026-11-09): feeToken
// 0x20c0…0000, decimals() 6. The contract's own symbol() is nUSD; the
// chain's documentation names the asset pathUSD, which is what users are
// told to hold.
var knownFeeTokens = map[int64]FeeToken{
	devnetChainID: {
		Symbol:   "pathUSD",
		Decimals: 6,
		Address:  "0x20c0000000000000000000000000000000000000",
	},
}
