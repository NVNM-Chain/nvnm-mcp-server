// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
)

// RepoRoot walks up from the working directory to the module root (go.mod).
// Tool subpackages and the harness can run with different cwds, so credentials
// and ABI paths must not assume they live a fixed number of levels down.
func RepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
