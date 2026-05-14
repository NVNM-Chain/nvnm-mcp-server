package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// keyFieldLiteral matches a `Key:` struct-field assignment inside a
// composite literal (e.g. `KeyEntry{ID: "x", Key: "raw", ...}`). The
// leading word boundary keeps it from matching the sibling fields
// KeyHash and KeyPrefix; requiring a following quote keeps it from
// matching prose like "Key: set" in doc comments.
var keyFieldLiteral = regexp.MustCompile(`(?m)\bKey:\s*"`)

// migrationRegressionFile is the one file allowed to construct
// KeyEntry values with the raw Key field set: its tests deliberately
// reconstruct the pre-8.6 on-disk shape to prove migrateLegacyEntries
// runs exactly once. See that file's header comment for the rationale.
const migrationRegressionFile = "keys_migration_test.go"

// TestNoRawKeyLiteralsOutsideMigrationTests enforces the constraint
// documented on NewKeyEntry in keys.go: production code and
// non-migration tests must build KeyEntry values through NewKeyEntry,
// never via a struct literal that sets the raw Key field. NewKeyEntry
// hashes the key once and never retains the raw bytes; a literal that
// sets Key bypasses that and risks the raw token reaching disk.
//
// This is the grep-enforcement that keys.go's doc comment promises CI
// would run. It is a plain *_test.go so it travels with `make test`
// and `go test ./...` rather than depending on CI YAML.
func TestNoRawKeyLiteralsOutsideMigrationTests(t *testing.T) {
	// Discover this file's own name at runtime rather than hardcoding
	// it: this file's doc comment carries an example KeyEntry literal
	// that the regex below deliberately matches, so it must exclude
	// itself -- and that exclusion has to survive a future rename.
	_, thisPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot identify this test file to exclude it")
	}
	thisFile := filepath.Base(thisPath)

	dirEntries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	for _, de := range dirEntries {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if name == migrationRegressionFile || name == thisFile {
			continue
		}

		data, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		for _, loc := range keyFieldLiteral.FindAllIndex(data, -1) {
			line := 1 + strings.Count(string(data[:loc[0]]), "\n")
			t.Errorf("%s:%d sets KeyEntry.Key via a struct literal; use NewKeyEntry instead "+
				"(raw Key literals are reserved for %s)", name, line, migrationRegressionFile)
		}
	}
}
