// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	defiwallet "github.com/defiweb/go-eth/wallet"
)

const (
	// envPrivateKey is the environment form of the signing key. Release CI
	// supplies it as a secret; it wins over the local credentials file.
	envPrivateKey = "NVNM_TEST_PRIVATE_KEY" // pragma: allowlist secret -- env var name, no key material
	// envAddress is an optional Address line for the environment form.
	// When set it is cross-checked against the address derived from the key.
	envAddress = "NVNM_TEST_ADDRESS"
	// envRequireChain turns a local skip into a CI failure. Set in release
	// and nightly so a missing secret cannot report success.
	envRequireChain = "NVNM_E2E_REQUIRE_CHAIN"
)

func requireChain() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envRequireChain))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// SkipOrFail skips locally when a chain-backed prerequisite is missing, and
// fails when the run was asked to exercise the chain (release / nightly).
func SkipOrFail(t *testing.T, reason string) {
	t.Helper()
	if requireChain() {
		t.Fatalf("chain-backed suite was asked to run (%s=1) but cannot: %s", envRequireChain, reason)
	}
	t.Skip(reason)
}

func credentialsPath() string {
	if v := os.Getenv(envCredentials); v != "" {
		return v
	}
	if root := RepoRoot(); root != "" {
		return filepath.Join(root, ".chain_credentials.txt")
	}
	return defaultCredentialsPath
}

// LoadCredentials resolves the signing key from the environment first and
// the local credentials file second. The address is always derived from
// the key; a disagreeing Address line fails loudly.
func LoadCredentials(t *testing.T) (address string, key *defiwallet.PrivateKey) {
	t.Helper()

	keyHex := strings.TrimSpace(os.Getenv(envPrivateKey))
	addrHint := strings.TrimSpace(os.Getenv(envAddress))
	source := envPrivateKey

	if keyHex == "" {
		path := credentialsPath()
		data, err := os.ReadFile(path) //nolint:gosec // test fixture path, operator-controlled
		if err != nil {
			SkipOrFail(t, fmt.Sprintf("%s unset and credentials file not found (%s): %v",
				envPrivateKey, path, err))
		}
		source = path
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "Address:"); ok {
				addrHint = strings.TrimSpace(after)
			}
			if after, ok := strings.CutPrefix(line, "PrivateKey:"); ok {
				keyHex = strings.TrimSpace(after)
			}
		}
		if keyHex == "" {
			t.Fatalf("credentials file %s is missing a PrivateKey line", path)
		}
	}

	keyBytes, err := hex.DecodeString(strings.TrimPrefix(keyHex, "0x"))
	if err != nil {
		t.Fatalf("invalid private key hex from %s: %v", source, err)
	}
	key = defiwallet.NewKeyFromBytes(keyBytes)

	derived := key.Address()
	if addrHint != "" && !strings.EqualFold(strings.TrimSpace(addrHint), derived.String()) {
		t.Fatalf("%s is inconsistent: Address says %s but PrivateKey derives %s",
			source, addrHint, derived.String())
	}
	return derived.String(), key
}
