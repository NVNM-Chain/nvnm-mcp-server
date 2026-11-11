// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

// ChainEnvironment identifies which NVNM Chain environment the server is
// configured against. Token naming and certain operator-facing URLs vary
// between environments.
type ChainEnvironment string

const (
	// EnvTestnet is the testnet environment. Native token mantraUSD, gas
	// token wmantraUSD.
	EnvTestnet ChainEnvironment = "testnet"
	// EnvMainnet is the mainnet environment. Native token mmUSD, gas
	// token wmmUSD.
	EnvMainnet ChainEnvironment = "mainnet"
)

// IsValid reports whether e is one of the two recognized environments.
func (e ChainEnvironment) IsValid() bool {
	return e == EnvTestnet || e == EnvMainnet
}

// String satisfies the Stringer interface.
func (e ChainEnvironment) String() string { return string(e) }

// TokenNaming carries the customer-facing token symbols for a given
// environment. The native form (e.g. mantraUSD) is what users on the
// origin chain hold; the wrapped form (e.g. wmantraUSD) is what an EVM
// wallet on NVNM holds and pays gas in.
type TokenNaming struct {
	// Native is the symbol of the native (non-gas) token, e.g. mantraUSD.
	Native string
	// Wrapped is the symbol of the wrapped gas token held in EVM wallets,
	// e.g. wmantraUSD.
	Wrapped string
}

// NamingFor returns the customer-facing token symbols for env. Defaults
// to testnet naming when env is empty or unrecognized, so callers can
// always render a balance without nil-checking.
func NamingFor(env ChainEnvironment) TokenNaming {
	if env == EnvMainnet {
		return TokenNaming{Native: "mmUSD", Wrapped: "wmmUSD"}
	}
	return TokenNaming{Native: "mantraUSD", Wrapped: "wmantraUSD"}
}

// EVM chain IDs the server recognizes for environment inference. These
// are the canonical NVNM Chain IDs per the network's documentation; an
// operator running against a private fork can still set
// NVNM_CHAIN_ENVIRONMENT explicitly.
const (
	testnetChainID int64 = 787111
	mainnetChainID int64 = 1611
	// l1DevnetChainID is nvnm-tempo-devnet-1, the L1 (a Tempo-based EVM
	// chain). It is a test network, so it infers EnvTestnet; what differs
	// from the L2 testnet is the fee asset (see FeeToken), not the
	// environment.
	l1DevnetChainID int64 = 787222
)

// InferEnvironmentFromChainID returns the environment a chain ID maps
// to, or an empty value if the chain ID is unrecognized. An empty
// return is the signal to fall back to operator-supplied configuration
// or to the testnet default.
func InferEnvironmentFromChainID(chainID int64) ChainEnvironment {
	switch chainID {
	case testnetChainID, l1DevnetChainID:
		return EnvTestnet
	case mainnetChainID:
		return EnvMainnet
	default:
		return ""
	}
}

// nativeCoinDecimals is the scale of an EVM native coin (wei).
const nativeCoinDecimals = 18

// maxFeeTokenDecimals bounds NVNM_FEE_TOKEN_DECIMALS. ERC-20 decimals is a
// uint8; nothing in use exceeds 18, and 36 leaves room without letting a
// typo produce a balance string hundreds of digits long.
const maxFeeTokenDecimals = 36

// FeeToken is the asset a wallet pays transaction fees in.
//
// The L2 charges fees in its native coin, read with eth_getBalance. The L1
// has no native coin: fees are charged in a token contract, and
// eth_getBalance answers the same placeholder for every address (observed
// 2026-11-09 on chain 787222), so the only meaningful balance is the
// token's balanceOf.
type FeeToken struct {
	// Symbol is the customer-facing name, e.g. pathUSD.
	Symbol string
	// Decimals is the token's scale: the balance integer divided by
	// 10^Decimals is the human amount.
	Decimals int
	// Address is the token contract. Empty means the native coin.
	Address string
}

// IsContract reports whether fees are charged in a token contract rather
// than the chain's native coin.
func (f FeeToken) IsContract() bool { return f.Address != "" }

// knownFeeTokens lists the chains whose fee asset is a token contract.
// Observed on a mined addRegistry receipt on chain 787222 (2026-11-09):
// feeToken 0x20c0…0000, decimals() 6. The contract's own symbol() is nUSD;
// the chain's documentation names the asset pathUSD, which is what users
// are told to hold.
var knownFeeTokens = map[int64]FeeToken{
	l1DevnetChainID: {
		Symbol:   "pathUSD",
		Decimals: 6,
		Address:  "0x20c0000000000000000000000000000000000000",
	},
}
