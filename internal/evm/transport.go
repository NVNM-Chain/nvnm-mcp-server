// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"fmt"
	"io"
	"net/http"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// maxNodeResponseBytes caps a single RPC response body. Node responses are
// untrusted (config permits a plaintext http:// endpoint), so a hostile or
// MITM'd node could otherwise stream an unbounded reply and exhaust process
// memory before the JSON decoder ever sees an error (EV-1). 32 MiB is far
// larger than any legitimate JSON-RPC response on this chain while keeping the
// per-request memory ceiling bounded.
const maxNodeResponseBytes = 32 << 20

// limitedTransport wraps an http.RoundTripper and caps the number of bytes any
// response body will yield. It is installed on the http.Client handed to the
// defiweb HTTP transport so the cap applies to every RPC call uniformly,
// independent of which decoder reads the body.
type limitedTransport struct {
	base  http.RoundTripper
	limit int64
}

func (t *limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &limitedReadCloser{inner: resp.Body, limit: t.limit}
	return resp, nil
}

// limitedReadCloser reads from inner until the cumulative byte count exceeds
// limit, at which point Read returns ErrNodeResponseTooLarge. It bounds memory
// to within one read buffer of limit -- enough to stop an unbounded stream
// without buffering the whole body first (unlike io.ReadAll of a raw body).
type limitedReadCloser struct {
	inner io.ReadCloser
	n     int64
	limit int64
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.inner.Read(p)
	l.n += int64(n)
	if l.n > l.limit {
		return n, fmt.Errorf("node response exceeded %d bytes: %w", l.limit, apperrors.ErrNodeResponseTooLarge)
	}
	return n, err
}

func (l *limitedReadCloser) Close() error { return l.inner.Close() }

// newLimitedHTTPClient returns an *http.Client whose transport caps every
// response body at maxNodeResponseBytes.
func newLimitedHTTPClient() *http.Client {
	return &http.Client{
		Transport: &limitedTransport{base: http.DefaultTransport, limit: maxNodeResponseBytes},
	}
}
