// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
)

func TestValidateKeyStore_DefaultFile_OK(t *testing.T) {
	c := &Config{AuthProvider: "apikey"} // KeyStoreBackend empty => file
	if err := c.validateKeyStore(); err != nil {
		t.Fatalf("default (file) keystore must validate: %v", err)
	}
}

func TestValidateKeyStore_InvalidBackend(t *testing.T) {
	c := &Config{AuthProvider: "apikey", KeyStoreBackend: "mysql"}
	if err := c.validateKeyStore(); !errors.Is(err, ErrInvalidKeyStoreBackend) {
		t.Fatalf("err = %v, want ErrInvalidKeyStoreBackend", err)
	}
}

func TestValidateKeyStore_PostgresRequiresDSN(t *testing.T) {
	c := &Config{AuthProvider: "apikey", KeyStoreBackend: "postgres", KeyHMACPepper: "p"}
	if err := c.validateKeyStore(); !errors.Is(err, ErrKeyStoreDSNRequired) {
		t.Fatalf("err = %v, want ErrKeyStoreDSNRequired", err)
	}
}

func TestValidateKeyStore_PostgresApikeyRequiresPepper(t *testing.T) {
	c := &Config{AuthProvider: "apikey", KeyStoreBackend: "postgres", KeyStoreDSN: "postgres://x"}
	if err := c.validateKeyStore(); !errors.Is(err, ErrPepperRequired) {
		t.Fatalf("err = %v, want ErrPepperRequired (hard gate)", err)
	}
}

func TestValidateKeyStore_PostgresFusionAuthNoPepperNeeded(t *testing.T) {
	// FusionAuth doesn't use the key store, so the pepper gate must NOT fire.
	c := &Config{AuthProvider: "fusionauth", KeyStoreBackend: "postgres", KeyStoreDSN: "postgres://x"}
	if err := c.validateKeyStore(); err != nil {
		t.Fatalf("postgres+fusionauth must not require a pepper: %v", err)
	}
}

func TestLoad_ReadsKeyStoreEnv(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t) // mirror the helper used by existing Load tests
	t.Setenv("KEY_STORE_BACKEND", "postgres")
	t.Setenv("KEY_STORE_DSN", "postgres://u:p@h:5432/db")
	t.Setenv("KEY_HMAC_PEPPER", "an-active-pepper-of-at-least-32-chars!!")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KeyStoreBackend != "postgres" || cfg.KeyStoreDSN == "" {
		t.Fatalf("keystore env not loaded: backend=%q dsn-empty=%v", cfg.KeyStoreBackend, cfg.KeyStoreDSN == "")
	}
}
