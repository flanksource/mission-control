package auth

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
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

func MintPluginInvocationToken(user models.Person, pluginID uuid.UUID, roles ...string) (string, error) {
	return MintPluginInvocationTokenWithDepth(user, pluginID, 0, roles...)
}

func MintPluginInvocationTokenWithDepth(user models.Person, pluginID uuid.UUID, depth int, roles ...string) (string, error) {
	now := time.Now()
	claims := PluginInvocationClaims{
		Plugin: pluginID,
		Type:   pluginInvocationTokenType,
		Depth:  depth,
		Roles:  append([]string(nil), roles...),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    signing.Issuer,
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{string(signing.AudiencePluginInvocation)},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(PluginJWTTTL)),
		},
	}

	return signing.NewJWT(signing.AudiencePluginInvocation, &claims)
}

func VerifyPluginInvocationToken(tokenString string, pluginID uuid.UUID) (*PluginInvocationClaims, error) {
	claims := &PluginInvocationClaims{}
	if _, err := signing.ParseJWT(tokenString, claims, signing.AudiencePluginInvocation); err != nil {
		return nil, err
	}

	if claims.Type != pluginInvocationTokenType {
		return nil, fmt.Errorf("plugin invocation token has invalid type %q", claims.Type)
	}
	if claims.Plugin != pluginID {
		return nil, fmt.Errorf("plugin invocation token is for plugin %q, not %q", claims.Plugin, pluginID)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("plugin invocation token subject is required")
	}

	return claims, nil
}
