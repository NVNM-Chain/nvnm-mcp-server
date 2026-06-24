// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
)

func TestValidateAuth_PreviousPepperWithoutActive_FailsLoud(t *testing.T) {
	c := &Config{
		AuthProvider:          "apikey",
		KeyHMACPepperPrevious: "old-pepper",
		// KeyHMACPepper deliberately empty
	}
	err := c.validateAuth()
	if !errors.Is(err, ErrPepperPreviousWithoutActive) {
		t.Fatalf("err = %v, want ErrPepperPreviousWithoutActive", err)
	}
}

func TestValidateAuth_BothPeppersSet_OK(t *testing.T) {
	c := &Config{
		AuthProvider:          "apikey",
		KeyHMACPepper:         "active",
		KeyHMACPepperPrevious: "old",
	}
	if err := c.validateAuth(); err != nil {
		t.Fatalf("validateAuth with both peppers = %v, want nil", err)
	}
}

func TestValidateAuth_NoPepper_OK(t *testing.T) {
	c := &Config{AuthProvider: "apikey"}
	if err := c.validateAuth(); err != nil {
		t.Fatalf("validateAuth with no pepper = %v, want nil (opt-in)", err)
	}
}

func TestLoad_ReadsPepperEnv(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("KEY_HMAC_PEPPER", "active-pepper")
	t.Setenv("KEY_HMAC_PEPPER_PREVIOUS", "prev-pepper")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KeyHMACPepper != "active-pepper" || cfg.KeyHMACPepperPrevious != "prev-pepper" {
		t.Fatalf("peppers not loaded: %q / %q", cfg.KeyHMACPepper, cfg.KeyHMACPepperPrevious)
	}
}
