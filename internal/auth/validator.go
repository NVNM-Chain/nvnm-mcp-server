package auth

// TokenValidator validates a bearer token and returns the authenticated claims.
// Implementations exist for API key lookup and FusionAuth JWT/JWKS validation.
type TokenValidator interface {
	Validate(token string) (*Claims, error)
	Close() error
}
