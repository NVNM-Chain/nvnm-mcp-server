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
		KeyHMACPepper:         "an-active-pepper-of-at-least-32-chars!!",
		KeyHMACPepperPrevious: "a-previous-pepper-of-at-least-32-chars!",
	}
	if err := c.validateAuth(); err != nil {
		t.Fatalf("validateAuth with both peppers = %v, want nil", err)
	}
}

func TestValidatePepperStrength_ShortActivePepper_FailsLoud(t *testing.T) {
	c := &Config{AuthProvider: "apikey", KeyHMACPepper: "too-short"}
	if err := c.validatePepperStrength(); !errors.Is(err, ErrPepperTooShort) {
		t.Fatalf("err = %v, want ErrPepperTooShort", err)
	}
}

func TestValidatePepperStrength_ShortPreviousPepper_FailsLoud(t *testing.T) {
	c := &Config{
		AuthProvider:          "apikey",
		KeyHMACPepper:         "an-active-pepper-of-at-least-32-chars!!",
		KeyHMACPepperPrevious: "short-prev",
	}
	if err := c.validatePepperStrength(); !errors.Is(err, ErrPepperTooShort) {
		t.Fatalf("err = %v, want ErrPepperTooShort for a short previous pepper", err)
	}
}

func TestValidatePepperStrength_LongPeppers_OK(t *testing.T) {
	c := &Config{
		KeyHMACPepper:         "an-active-pepper-of-at-least-32-chars!!",
		KeyHMACPepperPrevious: "a-previous-pepper-of-at-least-32-chars!",
	}
	if err := c.validatePepperStrength(); err != nil {
		t.Fatalf("validatePepperStrength with long peppers = %v, want nil", err)
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
	t.Setenv("KEY_HMAC_PEPPER", "an-active-pepper-of-at-least-32-chars!!")
	t.Setenv("KEY_HMAC_PEPPER_PREVIOUS", "a-previous-pepper-of-at-least-32-chars!")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KeyHMACPepper != "an-active-pepper-of-at-least-32-chars!!" ||
		cfg.KeyHMACPepperPrevious != "a-previous-pepper-of-at-least-32-chars!" {
		t.Fatalf("peppers not loaded: %q / %q", cfg.KeyHMACPepper, cfg.KeyHMACPepperPrevious)
	}
}
