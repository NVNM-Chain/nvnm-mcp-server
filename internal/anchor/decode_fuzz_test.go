// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"bytes"
	"path/filepath"
	"testing"
)

// FuzzAnchorDecodeValues drives arbitrary bytes through the anchor ABI decode
// boundary the way a hostile or MITM'd precompile/node would: the `output` of
// an eth_call is attacker-influenced, and defiweb's ABI decoder can panic on
// malformed length/offset prefixes. guardABIDecode must convert any such panic
// into an error rather than letting it crash the stdio process (EV-2, anchor
// leg). The fuzz runner turns any escaped panic into a test failure with the
// crashing input, so a clean run is positive evidence the guard holds across
// the input space -- the empirical complement to the two hand-crafted inputs
// that motivated the guard.
func FuzzAnchorDecodeValues(f *testing.F) {
	parsed, err := loadABI(filepath.Join("..", "..", "abi", "anchoring.json"))
	if err != nil {
		f.Skipf("load ABI: %v", err)
	}
	m := parsed.Methods["registries"]
	if m == nil {
		f.Skip("registries method not in ABI")
	}

	// Seeds: empty, an all-0xff head (huge offset), a valid-looking offset+huge
	// length, and a well-formed-ish tuple head.
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 64))
	longLen := make([]byte, 96)
	longLen[31] = 0x40
	longLen[63] = 0x40
	longLen[64+31] = 0x7f
	f.Add(longLen)
	// A 29-byte blob that fuzzing found panics the *unguarded* defiweb decoder
	// with "slice bounds out of range [-8388608:]" (a length prefix decoded as a
	// negative index). Kept as an explicit regression seed: guardABIDecode must
	// turn this real hostile-node crash into an error, not a process abort.
	f.Add(append(make([]byte, 28), 0xf0))

	f.Fuzz(func(_ *testing.T, out []byte) {
		var rows []abiRegistryRow
		var page abiPaginationOutput
		// Must not panic: guardABIDecode recovers any defiweb decoder panic and
		// returns ErrNodeResponseDecode. An escaped panic fails the fuzz run.
		_ = guardABIDecode(func() error {
			return m.DecodeValues(out, &rows, &page)
		})
	})
}
