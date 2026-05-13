package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func tempKeysFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "keys.json")
}

func TestManagedKeyStore_CreateAndLookup(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create("client-a", "required", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Key == "" {
		t.Fatal("expected raw key in create result")
	}
	if result.ID != "client-a" {
		t.Fatalf("got client_id %q, want client-a", result.ID)
	}

	entry := mks.Lookup(result.Key)
	if entry == nil {
		t.Fatal("expected Lookup to find newly created key")
	}
	if entry.ID != "client-a" {
		t.Fatalf("got ID %q, want client-a", entry.ID)
	}
}

func TestManagedKeyStore_CreateDuplicate(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mks.Create("client-a", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create("client-a", "", nil); err == nil {
		t.Fatal("expected error for duplicate client ID")
	}
}

func TestManagedKeyStore_List(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mks.Create("alpha", "auto", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create("beta", "required", nil); err != nil {
		t.Fatal(err)
	}

	summaries := mks.List()
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	for _, s := range summaries {
		if len(s.KeyPrefix) > 11 {
			t.Fatalf("List appears to have full key for %q (prefix too long: %d chars)", s.ID, len(s.KeyPrefix))
		}
	}
}

func TestManagedKeyStore_UpdateEnabled(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create("client-a", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	if entry := mks.Lookup(rawKey); entry == nil {
		t.Fatal("expected key to be findable before disable")
	}

	disabled := false
	if _, err := mks.Update("client-a", KeyUpdate{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}

	if entry := mks.Lookup(rawKey); entry != nil {
		t.Fatal("expected disabled key to be nil on Lookup")
	}

	enabled := true
	if _, err := mks.Update("client-a", KeyUpdate{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}

	if entry := mks.Lookup(rawKey); entry == nil {
		t.Fatal("expected re-enabled key to be findable")
	}
}

func TestManagedKeyStore_UpdateApproval(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create("client-a", "required", nil)
	if err != nil {
		t.Fatal(err)
	}

	auto := "auto"
	summary, err := mks.Update("client-a", KeyUpdate{WriteApproval: &auto})
	if err != nil {
		t.Fatal(err)
	}
	if summary.WriteApproval != "auto" {
		t.Fatalf("got write_approval %q, want auto", summary.WriteApproval)
	}

	entry := mks.Lookup(result.Key)
	if entry == nil {
		t.Fatal("expected key to be findable")
	}
	if entry.WriteApproval != "auto" {
		t.Fatalf("got write_approval %q in entry, want auto", entry.WriteApproval)
	}
}

func TestManagedKeyStore_UpdateMissing(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	enabled := true
	_, err = mks.Update("nonexistent", KeyUpdate{Enabled: &enabled})
	if err == nil {
		t.Fatal("expected error for missing client")
	}
}

func TestManagedKeyStore_Delete(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create("client-a", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	if err := mks.Delete("client-a"); err != nil {
		t.Fatal(err)
	}

	if entry := mks.Lookup(rawKey); entry != nil {
		t.Fatal("expected deleted key to be nil on Lookup")
	}

	if mks.TotalCount() != 0 {
		t.Fatalf("got total count %d, want 0", mks.TotalCount())
	}
}

func TestManagedKeyStore_DeleteMissing(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mks.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for missing client")
	}
}

func TestManagedKeyStore_PersistenceAcrossReloads(t *testing.T) {
	path := tempKeysFile(t)

	mks1, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks1.Create("persistent-client", "auto", nil)
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	mks2, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	entry := mks2.Lookup(rawKey)
	if entry == nil {
		t.Fatal("expected key to survive reload from disk")
	}
	if entry.ID != "persistent-client" {
		t.Fatalf("got ID %q, want persistent-client", entry.ID)
	}
	if entry.WriteApproval != "auto" {
		t.Fatalf("got write_approval %q, want auto", entry.WriteApproval)
	}
}

func TestManagedKeyStore_Counters(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mks.Create("a", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create("b", "", nil); err != nil {
		t.Fatal(err)
	}

	if mks.TotalCount() != 2 {
		t.Fatalf("got total %d, want 2", mks.TotalCount())
	}
	if mks.ActiveCount() != 2 {
		t.Fatalf("got active %d, want 2", mks.ActiveCount())
	}

	disabled := false
	if _, err := mks.Update("a", KeyUpdate{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if mks.ActiveCount() != 1 {
		t.Fatalf("got active %d after disable, want 1", mks.ActiveCount())
	}
	if mks.TotalCount() != 2 {
		t.Fatalf("got total %d after disable, want 2", mks.TotalCount())
	}
}

func TestManagedKeyStore_EmptyOnNewFile(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !mks.Empty() {
		t.Fatal("expected new store to be empty")
	}
	if mks.TotalCount() != 0 {
		t.Fatalf("got total %d, want 0", mks.TotalCount())
	}
}

func TestManagedKeyStore_FilePermissions(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, createErr := mks.Create("test", "", nil); createErr != nil {
		t.Fatal(createErr)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Fatalf("got file perm %o, want 0600", perm)
	}
}
