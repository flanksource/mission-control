package auth

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/models"
	"github.com/golang-jwt/jwt/v4"
)

const (
	PluginInvocationTokenTTL      = 2 * time.Minute
	pluginInvocationTokenIssuer   = "mission-control"
	pluginInvocationTokenAudience = "mission-control-plugin-host"
	pluginInvocationTokenType     = "plugin-invocation"
)

type PluginInvocationClaims struct {
	Plugin string `json:"plugin"`
	Type   string `json:"typ"`
	jwt.RegisteredClaims
}

func MintPluginInvocationToken(user *models.Person, pluginID string) (string, error) {
	if user == nil {
		return "", fmt.Errorf("user is required")
	}
	if pluginID == "" {
		return "", fmt.Errorf("plugin id is required")
	}

	now := time.Now()
	claims := PluginInvocationClaims{
		Plugin: pluginID,
		Type:   pluginInvocationTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    pluginInvocationTokenIssuer,
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{pluginInvocationTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(PluginInvocationTokenTTL)),
		},
	}

	key, err := pluginInvocationSigningKey()
	if err != nil {
		return "", err
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
}

func VerifyPluginInvocationToken(tokenString, pluginID string) (*PluginInvocationClaims, error) {
	if tokenString == "" {
		return nil, fmt.Errorf("plugin invocation token is required")
	}
	if pluginID == "" {
		return nil, fmt.Errorf("plugin id is required")
	}

	key, err := pluginInvocationSigningKey()
	if err != nil {
		return nil, err
	}

	claims := &PluginInvocationClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %s", t.Header["alg"])
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("plugin invocation token is invalid")
	}
	if !claims.VerifyAudience(pluginInvocationTokenAudience, true) {
		return nil, fmt.Errorf("plugin invocation token has invalid audience")
	}
	if !claims.VerifyIssuer(pluginInvocationTokenIssuer, true) {
		return nil, fmt.Errorf("plugin invocation token has invalid issuer")
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

func pluginInvocationSigningKey() ([]byte, error) {
	if api.DefaultConfig.Postgrest.JWTSecret == "" {
		return nil, fmt.Errorf("postgrest jwt secret is required for plugin invocation tokens")
	}
	return []byte("plugin-invocation:" + api.DefaultConfig.Postgrest.JWTSecret), nil
}
