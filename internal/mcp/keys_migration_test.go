// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// Phase 8.6 migration regression tests. The KeyEntry literals with
// Key: set in this file are intentional: they reconstruct the pre-8.6
// on-disk shape so we can prove the migration runs once, never twice,
// and leaves a backup behind.

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLoadKeysFile_RejectsLegacyWriteApproval asserts the loader fails
// loud when a key entry still carries the retired write_approval field,
// rather than silently ignoring it. Server-side write approval was
// removed in Option 0; a stale write_approval in a deployed key store is
// exactly the silent-drift trap fail-loud is meant to catch (mirrors the
// INVENIAM_* hard-cut). The error must name the offending key id.
func TestLoadKeysFile_RejectsLegacyWriteApproval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	raw := `[{"id":"legacy-key","key_hash":"` + strings.Repeat("a", 64) +
		`","enabled":true,"write_approval":"auto","roles":["writer"]}]`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}

	_, err := LoadKeysFile(path)
	if !errors.Is(err, ErrLegacyKeyWriteApproval) {
		t.Fatalf("LoadKeysFile error = %v, want ErrLegacyKeyWriteApproval", err)
	}
	if !strings.Contains(err.Error(), "legacy-key") {
		t.Errorf("error %q should name the offending key id", err.Error())
	}
}

// TestMigration_LegacyFileMigratedAndBackedUp loads a pre-8.6 file
// (raw Key only, no KeyHash) and asserts that NewManagedKeyStore:
//
//  1. Writes a one-shot .pre-migration backup with the original bytes.
//  2. Rewrites the primary file in hashed form (no raw Key).
//  3. Yields a working Lookup that hashes the input.
func TestMigration_LegacyFileMigratedAndBackedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	legacy := []KeyEntry{
		{ID: "legacy-1", Key: "raw-bearer-token-aaaaaaaa", Enabled: true},
		{ID: "legacy-2", Key: "raw-bearer-token-bbbbbbbb", Enabled: true, Roles: []string{"reader"}},
	}
	writeJSONFile(t, path, legacy)
	originalBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	mks, err := NewManagedKeyStore(path, discardLogger())
	if err != nil {
		t.Fatalf("NewManagedKeyStore: %v", err)
	}

	// 1. Backup exists with exactly the original bytes.
	backupBytes, err := os.ReadFile(path + preMigrationBackupSuffix)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(backupBytes, originalBytes) {
		t.Errorf("backup contents differ from original\n  original: %s\n  backup:   %s",
			originalBytes, backupBytes)
	}

	// 2. Primary file no longer contains the raw bearer tokens.
	primaryBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read primary: %v", err)
	}
	for _, raw := range []string{"raw-bearer-token-aaaaaaaa", "raw-bearer-token-bbbbbbbb"} {
		if containsString(primaryBytes, raw) {
			t.Errorf("primary file still contains raw key %q after migration", raw)
		}
	}

	// 3. Lookup works post-migration with the raw key as input.
	entry := mks.Lookup("raw-bearer-token-aaaaaaaa")
	if entry == nil {
		t.Fatal("Lookup post-migration returned nil for legacy-1")
	}
	if entry.ID != "legacy-1" {
		t.Errorf("Lookup ID = %q, want legacy-1", entry.ID)
	}
	if entry.KeyHash != auth.HashKey("raw-bearer-token-aaaaaaaa") {
		t.Errorf("KeyHash mismatch after migration")
	}
	if entry.Key != "" {
		t.Errorf("Key should be cleared after migration, got %q", entry.Key)
	}
}

// TestMigration_ReloadIsNoop asserts that a second NewManagedKeyStore
// on an already-migrated file does not write a new backup (one-shot
// semantics) and does not modify the primary file.
func TestMigration_ReloadIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	legacy := []KeyEntry{
		{ID: "legacy-1", Key: "raw-token", Enabled: true},
	}
	writeJSONFile(t, path, legacy)

	// First load: migration runs.
	if _, err := NewManagedKeyStore(path, discardLogger()); err != nil {
		t.Fatalf("first load: %v", err)
	}
	backupBytesAfterFirst, err := os.ReadFile(path + preMigrationBackupSuffix)
	if err != nil {
		t.Fatalf("read backup after first load: %v", err)
	}
	primaryBytesAfterFirst, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read primary after first load: %v", err)
	}

	// Sanity-check: simulate someone writing junk into the backup to
	// confirm the second load does NOT overwrite it. The one-shot
	// rule says the first backup is the truest record; later runs
	// must leave it alone.
	if writeErr := os.WriteFile(path+preMigrationBackupSuffix, []byte("sentinel"), 0o600); writeErr != nil {
		t.Fatalf("overwrite backup: %v", writeErr)
	}

	// Second load: no migration should run; the sentinel survives.
	if _, loadErr := NewManagedKeyStore(path, discardLogger()); loadErr != nil {
		t.Fatalf("second load: %v", loadErr)
	}
	backupBytesAfterSecond, err := os.ReadFile(path + preMigrationBackupSuffix)
	if err != nil {
		t.Fatalf("read backup after second load: %v", err)
	}
	if !bytes.Equal(backupBytesAfterSecond, []byte("sentinel")) {
		t.Errorf("second load overwrote the one-shot backup; got %q", backupBytesAfterSecond)
	}
	primaryBytesAfterSecond, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read primary after second load: %v", err)
	}
	if !bytes.Equal(primaryBytesAfterSecond, primaryBytesAfterFirst) {
		t.Error("second load rewrote the primary file; should be a no-op")
	}
	_ = backupBytesAfterFirst // referenced for clarity above
}

// TestMigration_AlreadyHashedFileUntouched loads a file written in the
// new hashed form and confirms no backup is created and the primary
// is not rewritten.
func TestMigration_AlreadyHashedFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	entries := []KeyEntry{NewKeyEntry("new-1", "fresh-token", nil)}
	writeJSONFile(t, path, entries)

	if _, err := NewManagedKeyStore(path, discardLogger()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := os.Stat(path + preMigrationBackupSuffix); !os.IsNotExist(err) {
		t.Errorf("backup should not exist for an already-hashed file; got err=%v", err)
	}
}

// TestMigration_InterruptedWriteRecoveredFromTmp simulates a crash
// between SaveKeysFile's tmp-write and rename: the .tmp file is left
// behind with valid contents, and the primary file is corrupted.
// LoadKeysFile must fall through to .tmp recovery.
func TestMigration_InterruptedWriteRecoveredFromTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	// Primary: malformed JSON (simulating a partial write).
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed primary: %v", err)
	}
	// .tmp: the new contents that should have replaced the primary.
	good := []KeyEntry{NewKeyEntry("recovered", "good-token", nil)}
	writeJSONFile(t, path+".tmp", good)

	entries, err := LoadKeysFile(path)
	if err != nil {
		t.Fatalf("LoadKeysFile should recover from .tmp: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "recovered" {
		t.Errorf("LoadKeysFile returned %+v; want [recovered]", entries)
	}
}

// TestMigration_LegacyEntryWithoutKeyOrHashSkipped ensures we do not
// silently drop entries that have neither a raw Key nor a KeyHash:
// they survive in memory (with no way to authenticate) so the
// operator can notice and clean them up.
func TestMigration_LegacyEntryWithoutKeyOrHashSkipped(t *testing.T) {
	entries := []KeyEntry{
		{ID: "ghost", Enabled: true}, // no Key, no KeyHash
		{ID: "good", Key: "raw", Enabled: true},
	}
	changed, count := migrateLegacyEntries(entries)
	if !changed || count != 1 {
		t.Errorf("expected one migrated entry; got changed=%v count=%d", changed, count)
	}
	if entries[0].KeyHash != "" || entries[0].Key != "" {
		t.Errorf("ghost entry should remain empty; got %+v", entries[0])
	}
	if entries[1].KeyHash == "" {
		t.Error("good entry should have KeyHash populated after migration")
	}
}

func containsString(haystack []byte, needle string) bool {
	if needle == "" {
		return true
	}
	return bytes.Contains(haystack, []byte(needle))
}
