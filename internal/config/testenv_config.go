// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

// This file resolves the chain target (RPC endpoint, chain ID, environment
// and known on-chain fixtures) for live, integration-tagged tests, so no
// integration test hardcodes a specific network.
//
// Values come from the same NVNM_* variables the server itself reads, so
// one .env drives both the server and the live tests. When a variable is
// unset the public NVNM testnet defaults apply; a variable that is set
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

// Testnet defaults, used only when the matching variable is unset.
const (
	LiveDefaultRPCURL  = "https://evm.testnet.nvnmchain.io"
	LiveDefaultChainID = int64(787111)

	// liveDefaultTxHash is a permanent testnet tx: addRecord into
	// ElicitTest-rc6 (registry 741), sent by liveDefaultTxSender.
	liveDefaultTxHash   = "0x7e0b28b30916a3dcc14c4ffe9a5ed6be06acd3d4f4c618161fbb1e6c1acf2188"
	liveDefaultTxSender = "0x9f8a6425F7AD925701fE1CdF85fd883340b2A9CD"
)

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
// the built-in fixture is valid only on the default testnet; on any other
// chain ok is false and the caller should skip the test.
func LiveMinedTx(getenv func(string) string, chainID int64) (fx LiveTxFixture, ok bool, err error) {
	hash := strings.TrimSpace(getenv(LiveEnvTxHash))
	sender := strings.TrimSpace(getenv(LiveEnvTxSender))
	switch {
	case hash != "" && sender != "":
		return LiveTxFixture{Hash: hash, Sender: sender}, true, nil
	case hash != "" || sender != "":
		return LiveTxFixture{}, false, ErrLivePartialTxFixture
	case chainID == LiveDefaultChainID:
		return LiveTxFixture{Hash: liveDefaultTxHash, Sender: liveDefaultTxSender}, true, nil
	default:
		return LiveTxFixture{}, false, nil
	}
}
