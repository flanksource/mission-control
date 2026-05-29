package auth

import (
	"fmt"
	"time"

	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const pluginInvocationTokenType = "plugin-invocation"

var PluginJWTTTL = 5 * time.Minute

type PluginInvocationClaims struct {
	Plugin uuid.UUID `json:"pluginID"`
	Type   string    `json:"typ"`
	Depth  int       `json:"depth,omitempty"`
	Roles  []string  `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

func (c *PluginInvocationClaims) VerifyExpiresAt(cmp int64, req bool) bool {
	// NOTE: github.com/golang-jwt/jwt/v4 for whatever reason has a different function signature
	// for VerifyExpiresAt for jwt.RegisteredClaims.
	// That's why we need this adapter
	return c.RegisteredClaims.VerifyExpiresAt(time.Unix(cmp, 0), req)
}

func MintPluginInvocationToken(subject string, pluginID uuid.UUID, depth int, roles ...string) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("plugin invocation subject is required")
	}
	if pluginID == uuid.Nil {
		return "", fmt.Errorf("plugin id is required")
	}

	now := time.Now()
	claims := PluginInvocationClaims{
		Plugin: pluginID,
		Type:   pluginInvocationTokenType,
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

func VerifyPluginInvocationToken(tokenString string, pluginID uuid.UUID) (*PluginInvocationClaims, error) {
	claims, err := VerifyAnyPluginInvocationToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Plugin != pluginID {
		return nil, fmt.Errorf("plugin invocation token is for plugin %q, not %q", claims.Plugin, pluginID)
	}
	return claims, nil
}

func VerifyAnyPluginInvocationToken(tokenString string) (*PluginInvocationClaims, error) {
	claims := &PluginInvocationClaims{}
	if _, err := signing.ParseJWT(tokenString, claims, signing.AudiencePluginInvocation); err != nil {
		return nil, err
	}

	if claims.Type != pluginInvocationTokenType {
		return nil, fmt.Errorf("plugin invocation token has invalid type %q", claims.Type)
	}
	if claims.Plugin == uuid.Nil {
		return nil, fmt.Errorf("plugin invocation token plugin is required")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("plugin invocation token subject is required")
	}

	return claims, nil
}
