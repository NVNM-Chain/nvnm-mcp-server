// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package logging

import (
	"log/slog"
	"net/url"
	"strings"
)

// SafeAddr redacts an EVM address to show only the first 6 and last 4 characters.
// Example: "0x9f8a6425F7AD925701fE1CdF85fd883340b2A9CD" -> "0x9f8a...A9CD"
func SafeAddr(key, addr string) slog.Attr {
	if len(addr) <= 10 {
		return slog.String(key, addr)
	}
	return slog.String(key, addr[:6]+"..."+addr[len(addr)-4:])
}

// SafeURL redacts a URL to show only the scheme and hostname.
// Example: "https://evm.inveniam.mantrachain.io/secret" -> "https://evm.inveniam.mantrachain.io"
func SafeURL(key, rawURL string) slog.Attr {
	u, err := url.Parse(rawURL)
	if err != nil {
		return slog.String(key, "[invalid_url]")
	}
	return slog.String(key, u.Scheme+"://"+u.Host)
}

// SafeTxData logs the length of transaction data rather than its content.
func SafeTxData(key, data string) slog.Attr {
	data = strings.TrimPrefix(data, "0x")
	return slog.Int(key+"_bytes", len(data)/2)
}
