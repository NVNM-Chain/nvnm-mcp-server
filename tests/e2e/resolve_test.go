// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestEncodeReverseRegistriesPeek(t *testing.T) {
	contract := loadAnchorABI(t)
	data, err := encodeReverseRegistriesPeek(contract)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("calldata length %d, want at least a 4-byte selector", len(data))
	}
	sel, ok := contract.Methods["registries"]
	if !ok || sel == nil {
		t.Fatal("registries method missing")
	}
	want := sel.FourBytes()
	if !bytes.Equal(data[:4], want[:]) {
		t.Errorf("selector = 0x%s, want 0x%s", hex.EncodeToString(data[:4]), hex.EncodeToString(want[:]))
	}
}

func TestToolCallTimeout_RegistriesListing(t *testing.T) {
	if got := toolCallTimeout("anchor_get_registries"); got != RegistriesTimeout {
		t.Errorf("anchor_get_registries timeout = %s, want %s", got, RegistriesTimeout)
	}
	if got := toolCallTimeout("anchor_get_registry"); got != HTTPTimeout {
		t.Errorf("anchor_get_registry timeout = %s, want %s", got, HTTPTimeout)
	}
}

func TestRegistriesLatencyBudget_BelowHTTPWait(t *testing.T) {
	if RegistriesLatencyBudget >= RegistriesTimeout {
		t.Errorf("latency budget %s must be below HTTP wait %s",
			RegistriesLatencyBudget, RegistriesTimeout)
	}
}
