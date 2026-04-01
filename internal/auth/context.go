package auth

import "context"

type clientIDKey struct{}
type writeApprovalKey struct{}

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

// WriteApprovalFromContext returns the per-client write approval policy
// ("required", "auto", or "" for unset/use-global-default).
func WriteApprovalFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(writeApprovalKey{}).(string); ok {
		return v
	}
	return ""
}

// ContextWithWriteApproval returns a derived context carrying the per-client
// write approval policy.
func ContextWithWriteApproval(ctx context.Context, policy string) context.Context {
	return context.WithValue(ctx, writeApprovalKey{}, policy)
}
