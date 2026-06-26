// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import "errors"

// RejectReason classifies why a key lookup did not yield a usable identity.
// It lets the auth layer return a self-explaining message to a caller who
// presented a real key, while keeping the no-match path generic.
type RejectReason int

const (
	// RejectNone means the key matched a usable, enabled, unexpired row.
	RejectNone RejectReason = iota
	// RejectNotFound means no stored row matched the presented key.
	RejectNotFound
	// RejectExpired means a row matched but its expires_at has passed.
	RejectExpired
	// RejectRevoked means a row matched but it is disabled.
	RejectRevoked
)

var (
	// ErrKeyExpired is returned when a presented API key matched a row whose
	// expiry has passed.
	ErrKeyExpired = errors.New("api key expired")
	// ErrKeyRevoked is returned when a presented API key matched a disabled row.
	ErrKeyRevoked = errors.New("api key revoked")
)
