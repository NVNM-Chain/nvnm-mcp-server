// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
)

var (
	// ErrAdminKeyEmptyID is returned when an ADMIN_API_KEYS_FILE row has no id.
	ErrAdminKeyEmptyID = errors.New("admin key entry has empty id")
	// ErrAdminKeyEmptyKey is returned when an ADMIN_API_KEYS_FILE row has no key.
	ErrAdminKeyEmptyKey = errors.New("admin key entry has empty key")
	// ErrAdminKeyDuplicateID is returned when two ADMIN_API_KEYS_FILE rows
	// share an id.
	ErrAdminKeyDuplicateID = errors.New("admin key entry has duplicate id")
	// ErrAdminKeyDuplicateHash is returned when two ADMIN_API_KEYS_FILE
	// rows (or a row and the AdminAPIKey seed) hash to the same key.
	ErrAdminKeyDuplicateHash = errors.New("admin key entry has duplicate key")
	// ErrAdminKeyNoneConfigured is returned when neither AdminAPIKey nor
	// AdminAPIKeysFile yields any usable entry (e.g. an empty-array
	// ADMIN_API_KEYS_FILE with no ADMIN_API_KEY seed). Without this
	// check the admin server would start with a zero-entry key map --
	// every request 401s and the misconfiguration is silent.
	ErrAdminKeyNoneConfigured = errors.New(
		"no admin keys configured (ADMIN_API_KEY empty and ADMIN_API_KEYS_FILE has no entries)")
)

// adminKeyEntry is one row of the ADMIN_API_KEYS_FILE JSON array: an admin
// identity and the raw bearer key that authenticates as that identity.
type adminKeyEntry struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// loadAdminKeys builds the admin identity map: sha256(key) -> admin id.
//
// When cfg.AdminAPIKey is set, it seeds a single entry mapping that key's
// hash to the fixed identity "admin" (the legacy single-shared-key
// behavior). When cfg.AdminAPIKeysFile is set, one additional entry is
// added per {id,key} row in the file, so admin-API callers can be
// attributed to an individual instead of collapsing to "admin".
//
// loadAdminKeys errors if the file is unreadable or not valid JSON, or if
// any row has an empty id or key, or if two rows share an id or a key
// hash (including collision with the AdminAPIKey seed), or if the
// resulting map is empty (ErrAdminKeyNoneConfigured) -- e.g. an
// ADMIN_API_KEYS_FILE containing `[]` and no ADMIN_API_KEY seed, which
// would otherwise start an admin server where every request 401s.
// Error messages never include the raw key material.
func loadAdminKeys(cfg *config.Config) (map[[32]byte]string, error) {
	keys := make(map[[32]byte]string)
	ids := make(map[string]struct{})

	if cfg.AdminAPIKey != "" {
		ids["admin"] = struct{}{}
		keys[sha256.Sum256([]byte(cfg.AdminAPIKey))] = "admin"
	}

	if cfg.AdminAPIKeysFile != "" {
		raw, err := os.ReadFile(cfg.AdminAPIKeysFile)
		if err != nil {
			return nil, fmt.Errorf("read admin keys file: %w", err)
		}

		var entries []adminKeyEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("parse admin keys file: %w", err)
		}

		for _, e := range entries {
			if e.ID == "" {
				return nil, fmt.Errorf("%w: file %q", ErrAdminKeyEmptyID, cfg.AdminAPIKeysFile)
			}
			if e.Key == "" {
				return nil, fmt.Errorf("%w: file %q, id %q", ErrAdminKeyEmptyKey, cfg.AdminAPIKeysFile, e.ID)
			}
			if _, dup := ids[e.ID]; dup {
				return nil, fmt.Errorf("%w: file %q, id %q", ErrAdminKeyDuplicateID, cfg.AdminAPIKeysFile, e.ID)
			}
			hash := sha256.Sum256([]byte(e.Key))
			if _, dup := keys[hash]; dup {
				return nil, fmt.Errorf("%w: file %q, id %q", ErrAdminKeyDuplicateHash, cfg.AdminAPIKeysFile, e.ID)
			}
			ids[e.ID] = struct{}{}
			keys[hash] = e.ID
		}
	}

	if len(keys) == 0 {
		return nil, ErrAdminKeyNoneConfigured
	}

	return keys, nil
}
