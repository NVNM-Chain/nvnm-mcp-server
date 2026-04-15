package auth

import "context"

type claimsKey struct{}

// ClaimsFromContext returns the authenticated Claims set by the auth middleware,
// or nil if unauthenticated (e.g. stdio transport).
func ClaimsFromContext(ctx context.Context) *Claims {
	if c, ok := ctx.Value(claimsKey{}).(*Claims); ok {
		return c
	}
	return nil
}

// ContextWithClaims returns a derived context carrying the authenticated claims.
func ContextWithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClientIDFromContext returns the authenticated client ID set by the auth
// middleware, or "" if unauthenticated (e.g. stdio transport).
func ClientIDFromContext(ctx context.Context) string {
	if c := ClaimsFromContext(ctx); c != nil {
		return c.ClientID
	}
	return ""
}

// WriteApprovalFromContext returns the per-client write approval policy
// ("required", "auto", or "" for unset/use-global-default).
func WriteApprovalFromContext(ctx context.Context) string {
	if c := ClaimsFromContext(ctx); c != nil {
		return c.WriteApproval
	}
	return ""
}
