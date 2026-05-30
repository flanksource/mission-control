package plugin

import (
	"fmt"
	"time"

	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

var PluginJWTTTL = 5 * time.Minute

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

func ValidateInvocationTokenForPlugin(tokenString string, pluginID uuid.UUID) (*InvocationTokenClaims, error) {
	claims, err := ValidateInvocationToken(tokenString)
	if err != nil {
		return nil, err
	}

	if claims.Plugin != pluginID {
		return nil, fmt.Errorf("plugin invocation token is for plugin %q, not %q", claims.Plugin, pluginID)
	}

	return claims, nil
}

func ValidateInvocationToken(tokenString string) (*InvocationTokenClaims, error) {
	claims := &InvocationTokenClaims{}
	if _, err := signing.ParseJWT(tokenString, claims, signing.AudiencePluginInvocation); err != nil {
		return nil, err
	}
	if claims.Plugin == uuid.Nil {
		return nil, fmt.Errorf("plugin invocation token plugin is required")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("plugin invocation token subject is required")
	}

	return claims, nil
}
