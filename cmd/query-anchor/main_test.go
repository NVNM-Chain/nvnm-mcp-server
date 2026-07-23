// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import "testing"

// TestPrintJSON exercises both the marshal-success and marshal-failure
// branches; printJSON writes to stdout and must not panic either way.
func TestPrintJSON(t *testing.T) {
	printJSON(map[string]int{"a": 1})
	// A channel is not JSON-serializable: exercises the error branch.
	printJSON(make(chan int))
}
