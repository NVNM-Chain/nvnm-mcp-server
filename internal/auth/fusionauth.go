package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// RoleAutomation is the role name that maps to automatic write approval.
const RoleAutomation = "automation"

// Sentinel errors for FusionAuth validation failures.
var (
	ErrMissingBaseURL  = errors.New("FusionAuth base URL is required")
	ErrMissingAppID    = errors.New("FusionAuth application ID is required")
	ErrTokenInvalid    = errors.New("token is not valid")
	ErrClaimsExtract   = errors.New("failed to extract claims")
	ErrInvalidIssuer   = errors.New("invalid issuer")
	ErrInvalidAudience = errors.New("invalid audience: token not issued for this application")
)

// FusionAuthConfig holds the settings needed to validate FusionAuth JWTs.
type FusionAuthConfig struct {
	BaseURL       string
	ApplicationID string
	Issuer        string
	JWKSURL       string
	ClockSkew     time.Duration
	RolesClaim    string
}

// FusionAuthValidator validates JWTs issued by FusionAuth using JWKS.
type FusionAuthValidator struct {
	config FusionAuthConfig
	logger *slog.Logger
	jwks   keyfunc.Keyfunc
	mu     sync.RWMutex
}

// NewFusionAuthValidator creates a validator that fetches JWKS keys on init.
func NewFusionAuthValidator(cfg *FusionAuthConfig, logger *slog.Logger) (*FusionAuthValidator, error) {
	if cfg.BaseURL == "" {
		return nil, ErrMissingBaseURL
	}
	if cfg.ApplicationID == "" {
		return nil, ErrMissingAppID
	}
	if cfg.RolesClaim == "" {
		cfg.RolesClaim = "roles"
	}
	if cfg.ClockSkew == 0 {
		cfg.ClockSkew = 60 * time.Second
	}

	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		jwksURL = strings.TrimRight(cfg.BaseURL, "/") + "/.well-known/jwks.json"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	jwks, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from %s: %w", jwksURL, err)
	}

	logger.Info("FusionAuth JWT validator initialized",
		slog.String("jwks_url", jwksURL),
		slog.String("application_id", cfg.ApplicationID),
	)

	return &FusionAuthValidator{
		config: *cfg,
		logger: logger,
		jwks:   jwks,
	}, nil
}

// Validate parses and validates a FusionAuth JWT, returning unified Claims.
func (v *FusionAuthValidator) Validate(tokenString string) (*Claims, error) {
	v.mu.RLock()
	jwks := v.jwks
	v.mu.RUnlock()

	token, err := jwt.Parse(tokenString, jwks.Keyfunc,
		jwt.WithLeeway(v.config.ClockSkew),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid {
		return nil, ErrTokenInvalid
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrClaimsExtract
	}

	expectedIssuer := v.config.Issuer
	if expectedIssuer == "" {
		expectedIssuer = v.config.BaseURL
	}
	if expectedIssuer != "" {
		issuer, issOK := mapClaims["iss"].(string)
		if !issOK || !matchIssuer(issuer, expectedIssuer) {
			return nil, fmt.Errorf(
				"%w: expected %s, got %s",
				ErrInvalidIssuer, expectedIssuer, issuer,
			)
		}
	}

	if !v.validateAudience(mapClaims) {
		return nil, ErrInvalidAudience
	}

	sub, _ := mapClaims["sub"].(string) //nolint:errcheck // sub is optional; empty string is valid
	roles := v.extractRoles(mapClaims)

	writeApproval := "required"
	for _, r := range roles {
		if r == RoleAutomation {
			writeApproval = "auto"
			break
		}
	}

	v.logger.Debug("validated FusionAuth token",
		slog.String("subject", sub),
		slog.Any("roles", roles),
	)

	return &Claims{
		ClientID:      sub,
		Roles:         roles,
		WriteApproval: writeApproval,
	}, nil
}

// Close releases JWKS resources.
func (v *FusionAuthValidator) Close() error {
	return nil
}

func (v *FusionAuthValidator) validateAudience(claims jwt.MapClaims) bool {
	aud, exists := claims["aud"]
	if !exists {
		return false
	}

	appID := v.config.ApplicationID

	switch audience := aud.(type) {
	case string:
		return audience == appID
	case []interface{}:
		for _, a := range audience {
			if s, ok := a.(string); ok && s == appID {
				return true
			}
		}
	}
	return false
}

func (v *FusionAuthValidator) extractRoles(claims jwt.MapClaims) []string {
	rolesClaim := v.config.RolesClaim

	if roles := extractRolesFromValue(claims[rolesClaim]); len(roles) > 0 {
		return roles
	}

	// FusionAuth nests roles under the application ID key.
	if v.config.ApplicationID != "" {
		if appData, ok := claims[v.config.ApplicationID].(map[string]interface{}); ok {
			if roles := extractRolesFromValue(appData["roles"]); len(roles) > 0 {
				return roles
			}
		}
	}

	return nil
}

// extractRolesFromValue processes a JWT claim value. The JWT library returns
// claims as interface{} regardless of the underlying type, making this unavoidable.
func extractRolesFromValue(val interface{}) []string {
	if val == nil {
		return nil
	}
	switch roles := val.(type) {
	case []interface{}:
		result := make([]string, 0, len(roles))
		for _, r := range roles {
			if s, ok := r.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return roles
	}
	return nil
}

// matchIssuer handles FusionAuth's quirk of sometimes stripping the scheme.
func matchIssuer(actual, expected string) bool {
	if actual == expected {
		return true
	}
	expectedNoScheme := strings.TrimPrefix(
		strings.TrimPrefix(expected, "https://"), "http://",
	)
	return actual == expectedNoScheme
}
