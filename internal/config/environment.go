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
)

// InferEnvironmentFromChainID returns the environment a chain ID maps
// to, or an empty value if the chain ID is unrecognized. An empty
// return is the signal to fall back to operator-supplied configuration
// or to the testnet default.
func InferEnvironmentFromChainID(chainID int64) ChainEnvironment {
	switch chainID {
	case testnetChainID:
		return EnvTestnet
	case mainnetChainID:
		return EnvMainnet
	default:
		return ""
	}
}
