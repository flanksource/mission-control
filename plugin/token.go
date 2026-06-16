package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

var PluginJWTTTL = 5 * time.Minute

// InvocationTokenClaims identifies the plugin invocation context carried between
// Mission Control, plugins, and plugin host callbacks.
type InvocationTokenClaims struct {
	Plugin uuid.UUID `json:"pluginID"`
	Depth  int       `json:"depth,omitempty"`
	Roles  []string  `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

func (c *InvocationTokenClaims) VerifyExpiresAt(cmp int64, req bool) bool {
	// NOTE: github.com/golang-jwt/jwt/v4 for whatever reason has a different function signature
	// for VerifyExpiresAt for jwt.RegisteredClaims.
	// That's why we need this adapter
	return c.RegisteredClaims.VerifyExpiresAt(time.Unix(cmp, 0), req)
}

// MintInvocationToken creates a short-lived token for invoking a specific plugin.
func MintInvocationToken(subject string, pluginID uuid.UUID, depth int, roles ...string) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("plugin invocation subject is required")
	}
	if pluginID == uuid.Nil {
		return "", fmt.Errorf("plugin id is required")
	}

	now := time.Now()
	claims := InvocationTokenClaims{
		Plugin: pluginID,
		Depth:  depth,
		Roles:  append([]string(nil), roles...),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    signing.Issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{string(signing.AudiencePluginInvocation)},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(PluginJWTTTL)),
		},
	}

	return signing.NewJWT(signing.AudiencePluginInvocation, &claims)
}

// ValidateInvocationToken validates a locally-signed invocation token without an
// expected plugin ID. Use this when the plugin identity must come from the token
// itself, such as plugin HostService callbacks, where the host resolves the
// calling plugin from claims.Plugin after validation.
func ValidateInvocationToken(tokenString string) (*InvocationTokenClaims, error) {
	claims := &InvocationTokenClaims{}
	if _, err := signing.ParseJWT(tokenString, claims, signing.AudiencePluginInvocation); err != nil {
		return nil, err
	}
	if err := validateInvocationTokenClaims(claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// ValidateRequestInvocationToken validates an invocation token for an HTTP/gRPC
// operation request where the route already determines the expected plugin. If
// the request came from upstream over the trusted tunnel, the upstream JWK is
// used; otherwise the local signing key is used.
func ValidateRequestInvocationToken(_ context.Context, token string, pluginID uuid.UUID) (*InvocationTokenClaims, error) {
	claims, err := ValidateInvocationToken(token)
	if err == nil {
		if claims.Plugin != pluginID {
			return nil, fmt.Errorf("plugin invocation token is for plugin %q, not %q", claims.Plugin, pluginID)
		}
		return claims, nil
	}

	if api.UpstreamConf.JWK != "" {
		return validateInvocationTokenWithJWK(token, api.UpstreamConf.JWK, &pluginID)
	}

	return nil, err
}

// ValidateHostInvocationToken validates tokens presented by a plugin to the host
// service. It accepts locally-minted tokens first, then upstream-minted tokens on
// agents. The plugin ID is intentionally taken from claims.Plugin because host
// callbacks do not have another trusted source for the calling plugin.
func ValidateHostInvocationToken(token string) (*InvocationTokenClaims, error) {
	claims, err := ValidateInvocationToken(token)
	if err == nil {
		return claims, nil
	}
	if api.UpstreamConf.Valid() && api.UpstreamConf.JWK != "" {
		return validateInvocationTokenWithJWK(token, api.UpstreamConf.JWK, nil)
	}
	return nil, err
}

func validateInvocationTokenWithJWK(tokenString, jwk string, pluginID *uuid.UUID) (*InvocationTokenClaims, error) {
	claims := &InvocationTokenClaims{}
	if _, err := signing.ParseJWTWithJWK(tokenString, claims, signing.AudiencePluginInvocation, jwk); err != nil {
		return nil, err
	}
	if err := validateInvocationTokenClaims(claims); err != nil {
		return nil, err
	}

	if pluginID != nil && claims.Plugin != *pluginID {
		return nil, fmt.Errorf("plugin invocation token is for plugin %q, not %q", claims.Plugin, *pluginID)
	}

	return claims, nil
}

func validateInvocationTokenClaims(claims *InvocationTokenClaims) error {
	if claims.Plugin == uuid.Nil {
		return fmt.Errorf("plugin invocation token plugin is required")
	}
	if claims.Subject == "" {
		return fmt.Errorf("plugin invocation token subject is required")
	}
	return nil
}
