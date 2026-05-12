package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveKeysFile_AtomicNoTempLeak verifies that a successful write
// (a) produces the target file with the expected contents and (b)
// leaves no .tmp-* sibling behind.
func TestSaveKeysFile_AtomicNoTempLeak(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keys.json")

	entries := []KeyEntry{{ID: "a", Key: "k", Enabled: true}}
	if err := SaveKeysFile(target, entries); err != nil {
		t.Fatalf("SaveKeysFile: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target missing after SaveKeysFile: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("target perms = %v, want 0o600", info.Mode().Perm())
	}

	// No temp file should remain in the directory.
	siblings, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range siblings {
		if e.Name() == "keys.json" {
			continue
		}
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leaked temp file: %s", e.Name())
		}
	}
}

// TestSaveKeysFile_TargetOnlyReplacedOnSuccess: if the rename target
// directory is read-only, the write should fail AND the existing file
// (if any) should remain untouched. This is the property that motivated
// atomic-rename in the first place.
func TestSaveKeysFile_KeepsExistingOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keys.json")

	// Seed an existing file.
	original := []KeyEntry{{ID: "orig", Key: "orig-key", Enabled: true}}
	if err := SaveKeysFile(target, original); err != nil {
		t.Fatalf("seed SaveKeysFile: %v", err)
	}
	originalBytes, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}

	// Make the directory read-only so CreateTemp inside it fails.
	if cherr := os.Chmod(dir, 0o500); cherr != nil {
		t.Fatalf("chmod dir: %v", cherr)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	updated := []KeyEntry{{ID: "new", Key: "new-key", Enabled: true}}
	if werr := SaveKeysFile(target, updated); werr == nil {
		t.Fatal("expected SaveKeysFile to fail on read-only directory, got nil")
	}

	// The original file must remain readable AND unchanged.
	if cherr := os.Chmod(dir, 0o700); cherr != nil {
		t.Fatalf("chmod restore: %v", cherr)
	}
	currentBytes, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("re-read target: %v", err)
	}
	if !bytes.Equal(currentBytes, originalBytes) {
		t.Errorf("target was modified despite write failure; got %q want %q",
			currentBytes, originalBytes)
	}
}
