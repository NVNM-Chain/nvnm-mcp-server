// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import "testing"

func TestLoadKeylessConfig_KeylessWrites(t *testing.T) {
	t.Setenv("MCP_KEYLESS_WRITES", "true")
	c := &Config{}
	if err := c.loadKeylessConfig(); err != nil {
		t.Fatalf("loadKeylessConfig: %v", err)
	}
	if !c.KeylessWrites {
		t.Error("KeylessWrites = false, want true")
	}
}

func TestLoadKeylessConfig_KeylessWritesDefaultsFalse(t *testing.T) {
	c := &Config{}
	if err := c.loadKeylessConfig(); err != nil {
		t.Fatalf("loadKeylessConfig: %v", err)
	}
	if c.KeylessWrites {
		t.Error("KeylessWrites = true, want false by default")
	}
}
