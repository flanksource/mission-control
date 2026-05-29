package auth

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	pluginInvocationTokenIssuer   = "mission-control"
	pluginInvocationTokenAudience = "mission-control-plugin-host"
	pluginInvocationTokenType     = "plugin-invocation"
)

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
			Issuer:    pluginInvocationTokenIssuer,
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{pluginInvocationTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(PluginJWTTTL)),
		},
	}

	key, _, err := signing.PrivateKey()
	if err != nil {
		return "", err
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
}

func VerifyPluginInvocationToken(tokenString string, pluginID uuid.UUID) (*PluginInvocationClaims, error) {
	key, _, err := signing.PublicKey()
	if err != nil {
		return nil, err
	}

	claims := &PluginInvocationClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unexpected signing method: got %v, expected RS256", t.Header["alg"])
		}
		return key, nil
	}, jwt.WithAudience(pluginInvocationTokenAudience), jwt.WithIssuer(pluginInvocationTokenIssuer))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("plugin invocation token is invalid")
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
