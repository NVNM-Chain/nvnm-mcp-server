// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "context"

// adminActorCtxKey is the unexported context key type used to store the
// resolved admin actor id on a request context. Using a dedicated type
// (rather than a bare string) avoids collisions with keys set by other
// packages sharing the same context.
type adminActorCtxKey struct{}

// contextWithAdminActor returns a copy of ctx carrying id as the resolved
// admin actor for the current request. Called by adminAuth once the
// presented bearer has been matched against the admin identity map.
func contextWithAdminActor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, adminActorCtxKey{}, id)
}

// adminActorFromContext returns the admin actor id resolved by adminAuth
// for the current request, or "admin" if no actor was set. The "admin"
// default preserves single-key back-compat: deployments running with a
// single shared admin key (no identity map) still attribute audit rows
// to a stable, non-empty actor.
func adminActorFromContext(ctx context.Context) string {
	id, ok := ctx.Value(adminActorCtxKey{}).(string)
	if !ok || id == "" {
		return "admin"
	}
	return id
}
