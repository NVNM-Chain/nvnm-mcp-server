// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import "context"

// TokenValidator validates a bearer token and returns the authenticated claims.
// Implementations exist for API key lookup and FusionAuth JWT/JWKS validation.
type TokenValidator interface {
	Validate(ctx context.Context, token string) (*Claims, error)
	Close() error
}
