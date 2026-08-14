// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

var update = flag.Bool("update", false, "update golden files")

func assertGolden(t *testing.T, name string, v any) {
	t.Helper()

	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", name+".golden.json")

	if *update {
		if writeErr := os.WriteFile(path, got, 0o644); writeErr != nil {
			t.Fatalf("update golden %s: %v", path, writeErr)
		}
		t.Logf("updated %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s golden mismatch.\nGot:\n%s\nWant:\n%s\n"+
			"Run: go test ./internal/mcp/ -run %s -update",
			name, got, want, t.Name())
	}
}

// TestGolden_BalanceOutput pins the public evm_get_balance response shape,
// including the chain-unit fields added alongside the legacy `ether` alias:
// balance_human restates the amount in the chain's own gas token and
// token_wrapped names that token. A field rename or type change here is a
// breaking change for every MCP client and must show up as a golden diff.
func TestGolden_BalanceOutput(t *testing.T) {
	out := balanceOutput{
		NormalizedBalance: evm.NormalizedBalance{
			Address: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD00",
			Wei:     "1000000000000000000",
			Ether:   "1.000000000000000000",
		},
		BalanceHuman: "1.000000000000000000 wmantraUSD",
		TokenWrapped: "wmantraUSD",
		NextActions: []NextAction{
			{Tool: "evm_get_transaction", Hint: "inspect a transaction from this address"},
		},
	}
	assertGolden(t, "balance_output", out)
}
