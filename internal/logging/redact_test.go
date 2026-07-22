// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package logging

import (
	"testing"
)

func TestSafeAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "standard 42-char address",
			addr: "0x9f8a6425F7AD925701fE1CdF85fd883340b2A9CD",
			want: "0x9f8a...A9CD",
		},
		{
			name: "short string unchanged",
			addr: "0x1234",
			want: "0x1234",
		},
		{
			name: "exactly 10 chars unchanged",
			addr: "0x12345678",
			want: "0x12345678",
		},
		{
			name: "11 chars truncated",
			addr: "0x123456789",
			want: "0x1234...6789",
		},
		{
			name: "empty string",
			addr: "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attr := SafeAddr("key", tc.addr)
			if attr.Value.String() != tc.want {
				t.Errorf("SafeAddr(%q) = %q, want %q", tc.addr, attr.Value.String(), tc.want)
			}
			if attr.Key != "key" {
				t.Errorf("key = %q, want %q", attr.Key, "key")
			}
		})
	}
}

func TestSafeEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{
			name:  "standard address keeps first char and domain",
			email: "alice@example.com",
			want:  "a***@example.com",
		},
		{
			name:  "subdomain preserved",
			email: "bob@mail.corp.example.io",
			want:  "b***@mail.corp.example.io",
		},
		{
			name:  "no at-sign is fully redacted",
			email: "not-an-email",
			want:  "[redacted_email]",
		},
		{
			name:  "empty is fully redacted",
			email: "",
			want:  "[redacted_email]",
		},
		{
			name:  "missing domain is fully redacted",
			email: "alice@",
			want:  "[redacted_email]",
		},
		{
			name:  "missing local part is fully redacted",
			email: "@example.com",
			want:  "[redacted_email]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attr := SafeEmail("email", tc.email)
			if attr.Value.String() != tc.want {
				t.Errorf("SafeEmail(%q) = %q, want %q", tc.email, attr.Value.String(), tc.want)
			}
			if attr.Key != "email" {
				t.Errorf("key = %q, want %q", attr.Key, "email")
			}
		})
	}
}

func TestSafeURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "strips path and query",
			url:  "https://evm.inveniam.mantrachain.io/secret?key=abc",
			want: "https://evm.inveniam.mantrachain.io",
		},
		{
			name: "preserves port",
			url:  "http://localhost:8545",
			want: "http://localhost:8545",
		},
		{
			name: "strips credentials",
			url:  "https://user:pass@host.example.com/rpc",
			want: "https://host.example.com",
		},
		{
			name: "invalid URL",
			url:  "://not-a-url",
			want: "[invalid_url]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attr := SafeURL("key", tc.url)
			if attr.Value.String() != tc.want {
				t.Errorf("SafeURL(%q) = %q, want %q", tc.url, attr.Value.String(), tc.want)
			}
		})
	}
}

func TestSafeTxData(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int64
	}{
		{
			name: "with 0x prefix",
			data: "0xabcdef01",
			want: 4,
		},
		{
			name: "without prefix",
			data: "abcdef01",
			want: 4,
		},
		{
			name: "empty",
			data: "",
			want: 0,
		},
		{
			name: "0x only",
			data: "0x",
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attr := SafeTxData("tx_data", tc.data)
			if attr.Value.Int64() != tc.want {
				t.Errorf("SafeTxData(%q) = %d, want %d", tc.data, attr.Value.Int64(), tc.want)
			}
			if attr.Key != "tx_data_bytes" {
				t.Errorf("key = %q, want %q", attr.Key, "tx_data_bytes")
			}
		})
	}
}
