// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	deficrypto "github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"
)

// Environment variables that describe a fee token contract. All three are
// optional on a chain listed in knownFeeTokens, where each one overrides
// the built-in value. On any other chain NVNM_FEE_TOKEN_ADDRESS switches
// the server to token balances and then the other two are required.
const (
	envFeeAssetAddress  = "NVNM_FEE_TOKEN_ADDRESS"
	envFeeAssetSymbol   = "NVNM_FEE_TOKEN_SYMBOL"
	envFeeAssetDecimals = "NVNM_FEE_TOKEN_DECIMALS"
)

// Fee-token configuration errors.
var (
	ErrFeeTokenAddressInvalid = errors.New(
		envFeeAssetAddress + " must be a 0x-prefixed 20-byte address; a mixed-case " +
			"address must carry a valid EIP-55 checksum (all-lowercase hex has none)")
	ErrFeeTokenDecimalsInvalid = errors.New(
		envFeeAssetDecimals + " must be an integer between 0 and " + strconv.Itoa(maxFeeTokenDecimals))
	// ErrFeeTokenIncomplete marks a fee token that is only partly
	// described. The symbol and the scale decide how every balance is
	// rendered, so neither is guessed.
	ErrFeeTokenIncomplete = errors.New(
		envFeeAssetAddress + ", " + envFeeAssetSymbol + " and " + envFeeAssetDecimals +
			" must be set together on a chain with no built-in fee token")
)

// FeeTokenFromEnv resolves the fee token contract for chainID from getenv
// (os.Getenv in production). The zero FeeToken means the chain charges
// fees in its native coin.
//
// A chain in knownFeeTokens starts from its built-in token and each set
// variable overrides one field. Any other chain is native-coin unless
// NVNM_FEE_TOKEN_ADDRESS is set, and then the symbol and decimals are
// required. A variable that is set but invalid is an error.
func FeeTokenFromEnv(getenv func(string) string, chainID int64) (FeeToken, error) {
	addr := strings.TrimSpace(getenv(envFeeAssetAddress))
	symbol := strings.TrimSpace(getenv(envFeeAssetSymbol))
	decimalsRaw := strings.TrimSpace(getenv(envFeeAssetDecimals))

	token, known := knownFeeTokens[chainID]
	if !known && addr == "" {
		if symbol != "" || decimalsRaw != "" {
			return FeeToken{}, ErrFeeTokenIncomplete
		}
		return FeeToken{}, nil
	}
	if !known && (symbol == "" || decimalsRaw == "") {
		return FeeToken{}, ErrFeeTokenIncomplete
	}

	if addr != "" {
		if !validFeeTokenAddress(addr) {
			return FeeToken{}, fmt.Errorf("%w: got %q", ErrFeeTokenAddressInvalid, addr)
		}
		token.Address = addr
	}
	if symbol != "" {
		token.Symbol = symbol
	}
	if decimalsRaw != "" {
		decimals, err := strconv.Atoi(decimalsRaw)
		if err != nil || decimals < 0 || decimals > maxFeeTokenDecimals {
			return FeeToken{}, fmt.Errorf("%w: got %q", ErrFeeTokenDecimalsInvalid, decimalsRaw)
		}
		token.Decimals = decimals
	}
	return token, nil
}

// validFeeTokenAddress applies the rule the balance read applies to the
// same string (evm.ParseAddress): 20 bytes of hex, and when the hex is
// mixed-case its casing must be the EIP-55 checksum. Lower- and upper-case
// hex carry no checksum. A bare hex parse is not enough: it accepts a
// mixed-case address with the wrong checksum, and the server would then
// start and fail every balance call instead of refusing to start.
//
// The rule is restated here because this package cannot import
// internal/evm: that package's tests import this one.
func validFeeTokenAddress(s string) bool {
	addr, err := defitypes.AddressFromHex(s)
	if err != nil {
		return false
	}
	hexPart := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if hexPart == strings.ToLower(hexPart) || hexPart == strings.ToUpper(hexPart) {
		return true
	}
	return "0x"+hexPart == addr.Checksum(deficrypto.Keccak256)
}

// GasToken returns the asset this deployment's wallets pay fees in: the
// configured fee token contract, or the native coin under its
// environment's name with 18 decimals.
func (c *Config) GasToken() FeeToken {
	if c.FeeToken.IsContract() {
		return c.FeeToken
	}
	return FeeToken{Symbol: NamingFor(c.ChainEnvironment).Wrapped, Decimals: nativeCoinDecimals}
}

// TokenNaming returns the customer-facing token symbols for this
// deployment. A chain whose fees are charged in a token contract has no
// native/wrapped pair: both names are the fee token, the one asset a
// wallet has to hold.
func (c *Config) TokenNaming() TokenNaming {
	if c.FeeToken.IsContract() {
		return TokenNaming{Native: c.FeeToken.Symbol, Wrapped: c.FeeToken.Symbol}
	}
	return NamingFor(c.ChainEnvironment)
}
