package auth

import "context"

type clientIDKey struct{}

// ClientIDFromContext returns the authenticated client ID set by the auth
// middleware, or "" if unauthenticated (e.g. stdio transport).
func ClientIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(clientIDKey{}).(string); ok {
		return id
	}
	return ""
}

// ContextWithClientID returns a derived context carrying the client identity.
func ContextWithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey{}, clientID)
}
