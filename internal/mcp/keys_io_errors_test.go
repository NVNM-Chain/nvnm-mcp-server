// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// badDirPath returns a path inside a directory that does not exist, so
// any atomic-write (CreateTemp) against it fails deterministically.
func badDirPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-such-dir", "file.json")
}

// --- keys.go: save / lock error paths ---

func TestSaveKeysFile_CreateTempFailure(t *testing.T) {
	entries := []KeyEntry{NewKeyEntry("c1", "raw-key-1", []string{"reader"})}
	if err := SaveKeysFile(badDirPath(t), entries); err == nil {
		t.Error("SaveKeysFile into a nonexistent directory should fail")
	}
}

func TestSaveKeysFile_PathIsDirectory(t *testing.T) {
	// Opening a directory O_RDWR fails with a non-ErrNotExist error,
	// covering withExclusiveLock's open-error branch.
	if err := SaveKeysFile(t.TempDir(), nil); err == nil {
		t.Error("SaveKeysFile onto a directory should fail")
	}
}

func TestSaveKeysFile_LockContention(t *testing.T) {
	path := tempKeysFile(t)
	if err := SaveKeysFile(path, nil); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("flock: %v", err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()

	if err := SaveKeysFile(path, nil); err == nil {
		t.Error("SaveKeysFile should fail while another holder has LOCK_EX")
	}
}

func TestKeyStore_EmptyAndMiss(t *testing.T) {
	ks := NewKeyStore(nil)
	if !ks.Empty() {
		t.Error("empty store should report Empty")
	}
	if got := ks.Lookup("nope"); got != nil {
		t.Errorf("Lookup on empty store = %v, want nil", got)
	}
}

func TestKeyLookupAdapter_RejectAndEmpty(t *testing.T) {
	mks := NewManagedKeyStoreFromEntries(tempKeysFile(t), nil)
	adapter := NewKeyLookupAdapter(mks)
	if !adapter.Empty() {
		t.Error("adapter over empty store should report Empty")
	}
	if res, reason := adapter.Lookup(context.Background(), "unknown"); res != nil || reason != auth.RejectNotFound {
		t.Errorf("Lookup = (%v, %v), want (nil, RejectNotFound)", res, reason)
	}
}

// --- managed_keys.go: load / migration / persist error paths ---

func TestNewManagedKeyStore_CorruptFile(t *testing.T) {
	path := tempKeysFile(t)
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedKeyStore(path, nil); err == nil {
		t.Error("corrupt keys file should fail store construction")
	}
}

// legacyKeysJSON is the pre-8.6 on-disk shape (raw key, no hash),
// written as JSON so migrateLegacyEntries has real work to do.
const legacyKeysJSON = `[{"id":"legacy-1","key":"legacy-raw-key-material","enabled":true,` +
	`"created_at":"2025-01-01T00:00:00Z"}]`

func TestManagedKeyStore_MigrationBackupWriteFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	if err := os.WriteFile(path, []byte(legacyKeysJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// Dangling symlink: Stat fails (no backup), WriteFile through the
	// link fails (target directory does not exist) -> warn branch 1,
	// disk stays in legacy shape.
	if err := os.Symlink(filepath.Join(dir, "no-such-dir", "backup"), path+preMigrationBackupSuffix); err != nil {
		t.Fatal(err)
	}

	mks, err := NewManagedKeyStore(path, testLogger())
	if err != nil {
		t.Fatalf("constructor should tolerate a failed backup: %v", err)
	}
	// In-memory state is normalized: the legacy key still authenticates.
	if e, reason := mks.Lookup(context.Background(), "legacy-raw-key-material"); e == nil || reason != auth.RejectNone {
		t.Errorf("legacy key lookup = (%v, %v), want hit", e, reason)
	}
	// The live file must be untouched (still contains the raw key).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "legacy-raw-key-material") {
		t.Error("live file should remain in pre-migration shape when the backup cannot be written")
	}
}

func TestManagedKeyStore_MigrationSaveFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	if err := os.WriteFile(path, []byte(legacyKeysJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// Pre-existing backup: the backup step is skipped (one-shot rule).
	if err := os.WriteFile(path+preMigrationBackupSuffix, []byte(legacyKeysJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold the advisory lock so the opportunistic re-save fails.
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("flock: %v", err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()

	mks, err := NewManagedKeyStore(path, testLogger())
	if err != nil {
		t.Fatalf("constructor should tolerate a failed re-save: %v", err)
	}
	if e, reason := mks.Lookup(context.Background(), "legacy-raw-key-material"); e == nil || reason != auth.RejectNone {
		t.Errorf("legacy key lookup = (%v, %v), want hit", e, reason)
	}
}

func TestWritePreMigrationBackup_Branches(t *testing.T) {
	dir := t.TempDir()

	// Original missing -> read error.
	if err := writePreMigrationBackup(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("missing original should fail")
	}

	// Backup path is a dangling symlink into a nonexistent directory:
	// Stat fails (no backup yet) and the WriteFile fails -> write error.
	path := filepath.Join(dir, "keys.json")
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "no-such-dir", "b"), path+preMigrationBackupSuffix); err != nil {
		t.Fatal(err)
	}
	if err := writePreMigrationBackup(path); err == nil {
		t.Error("unwritable backup path should fail")
	}

	// Existing regular backup -> no-op success.
	path2 := filepath.Join(dir, "keys2.json")
	if err := os.WriteFile(path2, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2+preMigrationBackupSuffix, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePreMigrationBackup(path2); err != nil {
		t.Errorf("existing backup should be a no-op, got %v", err)
	}
}

func TestManagedKeyStore_PersistFailures(t *testing.T) {
	entries := []KeyEntry{NewKeyEntry("c1", "raw-key-1", []string{"reader"})}
	mks := NewManagedKeyStoreFromEntries(badDirPath(t), entries)

	if _, err := mks.Create(context.Background(), "c2", []string{"reader"}, time.Time{}); err == nil {
		t.Error("Create should fail when persistence fails")
	}
	enabled := false
	if _, err := mks.Update("c1", KeyUpdate{Enabled: &enabled}); err == nil {
		t.Error("Update should fail when persistence fails")
	}
	if err := mks.Delete("c1"); err == nil {
		t.Error("Delete should fail when persistence fails")
	}
}

// --- keys_pending.go: load / persist error paths ---

func TestNewPendingKeyStore_ErrorPaths(t *testing.T) {
	if _, err := NewPendingKeyStore(""); !errors.Is(err, ErrPendingEmptyPath) {
		t.Errorf("empty path error = %v, want ErrPendingEmptyPath", err)
	}
	if _, err := NewPendingKeyStore(t.TempDir()); err == nil {
		t.Error("directory path should fail to load")
	}

	corrupt := filepath.Join(t.TempDir(), "pending.json")
	if err := os.WriteFile(corrupt, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPendingKeyStore(corrupt); err == nil {
		t.Error("corrupt pending file should fail to load")
	}

	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewPendingKeyStore(empty)
	if err != nil {
		t.Fatalf("empty file should load as empty store: %v", err)
	}
	if got := len(s.List()); got != 0 {
		t.Errorf("items = %d, want 0", got)
	}
}

func TestPendingKeyStore_AddRollsBackOnSaveFailure(t *testing.T) {
	s := badSavePendingStore(t)
	if _, err := s.Add("a@b.c", "", "testing", "127.0.0.1"); err == nil {
		t.Fatal("Add should fail when persistence fails")
	}
	if got := len(s.List()); got != 0 {
		t.Errorf("items after failed Add = %d, want 0 (rollback)", got)
	}
}

func TestPendingKeyStore_DecideRollsBackOnSaveFailure(t *testing.T) {
	s := badSavePendingStore(t, pendingReq("req-1", PendingStatusPending))
	if _, err := s.Decide("req-1", PendingStatusApproved, "admin", "key-1"); err == nil {
		t.Fatal("Decide should fail when persistence fails")
	}
	got, ok := s.Get("req-1")
	if !ok || got.Status != PendingStatusPending {
		t.Errorf("request after failed Decide = (%+v, %v), want pending (rollback)", got, ok)
	}
}

// --- keys_request_http.go: store failure + writer failure ---

func TestKeyRequest_StoreAddFailureReturns500(t *testing.T) {
	h := NewKeyRequestHandler(KeyRequestHandlerConfig{
		Store:  badSavePendingStore(t),
		Logger: testLogger(),
	})
	body := `{"email":"a@b.c","intended_use":"testing"}`
	req := httptest.NewRequest(http.MethodPost, KeyRequestPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestKeyRequestRateLimiter_Size(t *testing.T) {
	l := NewKeyRequestRateLimiter(1, 1, false)
	if got := l.Size(); got != 0 {
		t.Errorf("Size = %d, want 0", got)
	}
	if !l.allowIP("1.2.3.4") {
		t.Error("first request should be allowed")
	}
	if got := l.Size(); got != 1 {
		t.Errorf("Size = %d, want 1", got)
	}
}

func TestWriteJSONHelpers_WriterFailureLogged(t *testing.T) {
	// Contract: a broken writer is logged, never panics.
	writeKeyRequestJSON(newFailingResponseWriter(), testLogger(), http.StatusOK, map[string]string{"a": "b"})
	writeRateLimited(newFailingResponseWriter(), testLogger())
}
