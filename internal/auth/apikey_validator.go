package auth

import (
	"crypto/subtle"
	"errors"
)

// ErrInvalidAPIKey is returned when the API key is not found or disabled.
var ErrInvalidAPIKey = errors.New("invalid API key")

// KeyResult holds the fields needed from a key entry for authentication.
type KeyResult struct {
	ID            string
	Key           string
	WriteApproval string
}

// KeyLookup abstracts read-only key operations needed by the API key validator.
// Both *KeyStore and *ManagedKeyStore in the mcp package satisfy this interface.
type KeyLookup interface {
	Lookup(rawKey string) *KeyResult
	Empty() bool
}

// APIKeyValidator implements TokenValidator by looking up API keys
// in a KeyLookup store and performing constant-time comparison.
type APIKeyValidator struct {
	keys KeyLookup
}

// NewAPIKeyValidator creates a TokenValidator backed by API key lookup.
// Returns nil if keys is nil or empty (no authentication enforced).
func NewAPIKeyValidator(keys KeyLookup) *APIKeyValidator {
	if keys == nil || keys.Empty() {
		return nil
	}
	return &APIKeyValidator{keys: keys}
}

// Validate looks up the token in the key store and returns claims on success.
func (v *APIKeyValidator) Validate(token string) (*Claims, error) {
	entry := v.keys.Lookup(token)
	if entry == nil {
		return nil, ErrInvalidAPIKey
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(entry.Key)) != 1 {
		return nil, ErrInvalidAPIKey
	}

	return &Claims{
		ClientID:      entry.ID,
		WriteApproval: entry.WriteApproval,
	}, nil
}

// Close is a no-op for API key validation.
func (v *APIKeyValidator) Close() error { return nil }
