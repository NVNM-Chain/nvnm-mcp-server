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

// The chains the server knows without being told: for these the
// environment is inferred and the fee token is built in, so
// NVNM_EVM_RPC_URL and NVNM_CHAIN_ID are the only chain settings needed.
// Which chain a server runs against is still chosen by those two
// variables; nothing here selects a chain. Any other chain sets
// NVNM_CHAIN_ENVIRONMENT and the NVNM_FEE_TOKEN_* variables.
const (
	// testnetChainID is the NVNM testnet.
	testnetChainID int64 = 787223
	// devnetChainID is nvnm-tempo-devnet-1.
	devnetChainID int64 = 787222
)

// InferEnvironmentFromChainID returns the environment a chain ID maps
// to, or an empty value if the chain ID is unrecognized. An empty
// return is the signal that the operator must set
// NVNM_CHAIN_ENVIRONMENT.
func InferEnvironmentFromChainID(chainID int64) ChainEnvironment {
	switch chainID {
	case testnetChainID, devnetChainID:
		return EnvTestnet
	default:
		return ""
	}
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
	// Symbol is the customer-facing name, e.g. nUSD.
	Symbol string
	// Decimals is the token's scale: the balance integer divided by
	// 10^Decimals is the human amount.
	Decimals int
	// Address is the token contract.
	Address string
}

// nUSD is the fee token of the known chains. Observed on mined anchor
// receipts on chain 787222 (2026-11-09) and chain 787223 (2026-11-16):
// feeToken 0x20c0…0000, decimals() 6, and the contract's own name() and
// symbol() are nUSD.
var nUSD = FeeToken{
	Symbol:   "nUSD",
	Decimals: 6,
	Address:  "0x20c0000000000000000000000000000000000000",
}

// knownFeeTokens is the built-in fee token per known chain.
var knownFeeTokens = map[int64]FeeToken{
	testnetChainID: nUSD,
	devnetChainID:  nUSD,
}
