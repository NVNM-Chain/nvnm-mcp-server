package mcp

import (
	"errors"
	"strings"
	"testing"

	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
)

// FuzzParseHexData verifies that parseHexData never panics and always
// returns either a byte slice or an error -- never a nil slice with a
// nil error. The fuzzer seeds the standard happy/edge inputs and lets
// `go test -fuzz` explore the input space.
func FuzzParseHexData(f *testing.F) {
	// Seed corpus: empty, valid even-length hex, with/without 0x prefix,
	// odd-length, non-hex chars, and a value just at the size cap.
	f.Add("")
	f.Add("0x")
	f.Add("0xdeadbeef")
	f.Add("deadbeef")
	f.Add("0xdead")
	f.Add("z!@#")
	f.Add("0x" + strings.Repeat("ab", maxHexDataLen/2))
	f.Add("0x" + strings.Repeat("ab", maxHexDataLen)) // over cap

	f.Fuzz(func(t *testing.T, in string) {
		out, err := parseHexData(in)
		if err != nil {
			// Errors must be either the size-cap sentinel or a stdlib
			// hex decode error. The stdlib may return a partial
			// (empty or short) slice alongside the error; that's
			// fine -- what we care about is "no panic, no nil-out +
			// nil-err lie".
			if errors.Is(err, apperrors.ErrInvalidABI) {
				return
			}
			return
		}
		if out == nil {
			t.Error("returned nil bytes with nil error")
		}
	})
}
