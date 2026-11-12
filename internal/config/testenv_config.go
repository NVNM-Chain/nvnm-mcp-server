// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

// This file resolves the chain target (RPC endpoint, chain ID, environment
// and known on-chain fixtures) for live, integration-tagged tests, so no
// integration test hardcodes a specific network.
//
// Values come from the same NVNM_* variables the server itself reads, so
// one .env drives both the server and the live tests. When a variable is
// unset the devnet defaults apply; a variable that is set
// but invalid is an error (never silently replaced by a default).
//
// Every identifier carries a "Live" prefix to keep it distinct from the
// server's own configuration API in this package.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Environment variable names read by the live-test configuration.
const (
	LiveEnvRPCURL           = "NVNM_EVM_RPC_URL"
	LiveEnvChainID          = "NVNM_CHAIN_ID"
	LiveEnvChainEnvironment = "NVNM_CHAIN_ENVIRONMENT"
	// LiveEnvTxHash / LiveEnvTxSender name a mined transaction and its
	// sender on the target chain, for tests that read a known historical tx.
	LiveEnvTxHash   = "NVNM_TEST_TX_HASH"
	LiveEnvTxSender = "NVNM_TEST_TX_SENDER"
)

// Defaults, used only when the matching variable is unset: the devnet.
const (
	LiveDefaultRPCURL  = "https://rpc.nvnm.canary.mantrachain.dev"
	LiveDefaultChainID = devnetChainID
)

// liveBuiltinTxFixtures are the mined transactions known per chain, used
// when NVNM_TEST_TX_HASH / NVNM_TEST_TX_SENDER are unset. The devnet entry
// is the addRegistry that created registry 2227 (block 4881470, observed
// 2026-11-09). A devnet can be reset; when it is, override the fixture
// through the environment.
var liveBuiltinTxFixtures = map[int64]LiveTxFixture{
	devnetChainID: {
		Hash:   "0xc3139110f81470f527de4d482125186eb0ca7142b44bd9bbd5d677c2872fb13a",
		Sender: "0x50d476e5d2c044998eC185D86A0EF5CB32f3e8F4",
	},
}

// Errors returned for set-but-invalid live-test configuration.
var (
	ErrLiveInvalidChainID          = errors.New("invalid " + LiveEnvChainID)
	ErrLiveInvalidChainEnvironment = errors.New("invalid " + LiveEnvChainEnvironment)
	ErrLiveUnknownEnvironment      = errors.New("cannot determine chain environment")
	ErrLivePartialTxFixture        = errors.New(
		"tx fixture needs both " + LiveEnvTxHash + " and " + LiveEnvTxSender)
)

// LiveTarget is the chain a live test run talks to.
type LiveTarget struct {
	RPCURL  string
	ChainID int64
}

// LiveTargetFromEnv resolves the target from getenv (os.Getenv in production).
func LiveTargetFromEnv(getenv func(string) string) (LiveTarget, error) {
	t := LiveTarget{RPCURL: LiveDefaultRPCURL, ChainID: LiveDefaultChainID}
	if v := strings.TrimSpace(getenv(LiveEnvRPCURL)); v != "" {
		t.RPCURL = v
	}
	if v := strings.TrimSpace(getenv(LiveEnvChainID)); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return LiveTarget{}, fmt.Errorf("%w: %q must be a positive integer", ErrLiveInvalidChainID, v)
		}
		t.ChainID = id
	}
	return t, nil
}

// MustLiveTarget is LiveTargetFromEnv over the process environment. It
// panics on invalid configuration so a misconfigured run fails at package
// init instead of producing misleading test results.
func MustLiveTarget() LiveTarget {
	t, err := LiveTargetFromEnv(os.Getenv)
	if err != nil {
		panic(err)
	}
	return t
}

// LiveEnvironment returns the chain environment for chainID: the explicit
// NVNM_CHAIN_ENVIRONMENT when set, else inferred from the chain ID. An
// unrecognized chain ID with no explicit value is an error (same fail-fast
// rule as the server's config).
func LiveEnvironment(getenv func(string) string, chainID int64) (ChainEnvironment, error) {
	if raw := strings.TrimSpace(getenv(LiveEnvChainEnvironment)); raw != "" {
		env := ChainEnvironment(raw)
		if !env.IsValid() {
			return "", fmt.Errorf("%w: %q must be \"testnet\" or \"mainnet\"", ErrLiveInvalidChainEnvironment, raw)
		}
		return env, nil
	}
	if env := InferEnvironmentFromChainID(chainID); env != "" {
		return env, nil
	}
	return "", fmt.Errorf("%w for chain ID %d: set %s", ErrLiveUnknownEnvironment, chainID, LiveEnvChainEnvironment)
}

// LiveTxFixture identifies a mined transaction and its sender.
type LiveTxFixture struct {
	Hash   string
	Sender string
}

// LiveMinedTx returns the known mined-tx fixture for the target chain. Both
// NVNM_TEST_TX_HASH and NVNM_TEST_TX_SENDER override it. Without overrides
// the built-in fixture for the chain is used (liveBuiltinTxFixtures); on a
// chain with none ok is false and the caller should skip the test.
func LiveMinedTx(getenv func(string) string, chainID int64) (fx LiveTxFixture, ok bool, err error) {
	hash := strings.TrimSpace(getenv(LiveEnvTxHash))
	sender := strings.TrimSpace(getenv(LiveEnvTxSender))
	switch {
	case hash != "" && sender != "":
		return LiveTxFixture{Hash: hash, Sender: sender}, true, nil
	case hash != "" || sender != "":
		return LiveTxFixture{}, false, ErrLivePartialTxFixture
	}
	fx, ok = liveBuiltinTxFixtures[chainID]
	return fx, ok, nil
}
