package auth

// Claims holds the authenticated identity produced by any auth provider.
// Both API key validation and FusionAuth JWT validation produce this type.
type Claims struct {
	ClientID      string
	Roles         []string
	WriteApproval string // "required", "auto", or "" (use global default)
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
