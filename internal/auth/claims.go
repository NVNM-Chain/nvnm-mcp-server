// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

// Claims holds the authenticated identity produced by any auth provider.
// Both API key validation and FusionAuth JWT validation produce this type.
type Claims struct {
	ClientID string
	Roles    []string
}

// HasRole returns true if the claims include the specified role.
func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole returns true if the claims include at least one of the given roles.
func (c *Claims) HasAnyRole(roles ...string) bool {
	for _, required := range roles {
		if c.HasRole(required) {
			return true
		}
	}
	return false
}

// validRoles is the set of RBAC roles recognized by IsValidRole, used by
// config validation. (Key issuance in internal/mcp/admin.go currently keeps
// its own equivalent set; unifying them on IsValidRole is future work.)
var validRoles = map[string]struct{}{
	"reader":     {},
	"writer":     {},
	"admin":      {},
	"automation": {},
}

// IsValidRole reports whether role is one of the recognized RBAC roles.
func IsValidRole(role string) bool {
	_, ok := validRoles[role]
	return ok
}
