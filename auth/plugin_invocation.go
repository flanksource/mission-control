package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	pluginInvocationTokenIssuer   = "mission-control"
	pluginInvocationTokenAudience = "mission-control-plugin-host"
	pluginInvocationTokenType     = "plugin-invocation"
)

var (
	PluginJWTTTL = 5 * time.Minute

	// Signing secret
	PluginJWTSecret string
)

func init() {
	if PluginJWTSecret != "" {
		return
	}

	if secret := os.Getenv("PLUGIN_JWT_SECRET"); secret != "" {
		PluginJWTSecret = secret
		return
	}

	// Generate a random secret if not provided
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Errorf("generate plugin invocation jwt secret: %w", err))
	}
	PluginJWTSecret = base64.RawStdEncoding.EncodeToString(secret)
}

type PluginInvocationClaims struct {
	Plugin uuid.UUID `json:"pluginID"`
	Type   string    `json:"typ"`
	Depth  int       `json:"depth,omitempty"`
	jwt.RegisteredClaims
}

func MintPluginInvocationToken(user models.Person, pluginID uuid.UUID) (string, error) {
	return MintPluginInvocationTokenWithDepth(user, pluginID, 0)
}

func MintPluginInvocationTokenWithDepth(user models.Person, pluginID uuid.UUID, depth int) (string, error) {
	now := time.Now()
	claims := PluginInvocationClaims{
		Plugin: pluginID,
		Type:   pluginInvocationTokenType,
		Depth:  depth,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    pluginInvocationTokenIssuer,
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{pluginInvocationTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(PluginJWTTTL)),
		},
	}

	key, err := pluginInvocationSigningKey()
	if err != nil {
		return "", err
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
}

func VerifyPluginInvocationToken(tokenString string, pluginID uuid.UUID) (*PluginInvocationClaims, error) {
	key, err := pluginInvocationSigningKey()
	if err != nil {
		return nil, err
	}

	claims := &PluginInvocationClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %s", t.Header["alg"])
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

func pluginInvocationSigningKey() ([]byte, error) {
	if PluginJWTSecret == "" {
		return nil, fmt.Errorf("plugin invocation jwt secret is required")
	}

	return []byte(PluginJWTSecret), nil
}
