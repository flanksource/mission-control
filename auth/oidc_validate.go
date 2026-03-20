package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	oidcmodels "github.com/flanksource/incident-commander/auth/oidc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
)

var oidcPublicKeyCache = cache.New(5*time.Minute, 10*time.Minute)

// authenticateOIDCToken validates a Bearer JWT token against OIDC public keys.
// Returns (true, nil) if the token is valid, (false, nil) if it's not an OIDC token,
// and (false, err) on internal error.
func authenticateOIDCToken(c echo.Context, tokenStr string) (bool, error) {
	// Quick check: must have 3 parts (JWT format)
	if strings.Count(tokenStr, ".") != 2 {
		return false, nil
	}

	ctx := c.Request().Context().(context.Context)

	keys, err := loadOIDCPublicKeys(ctx)
	if err != nil || len(keys) == 0 {
		return false, nil
	}

	issuer := api.PublicURL

	var lastErr error
	for _, pub := range keys {
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return pub, nil
		})
		if err != nil {
			lastErr = err
			continue
		}
		if !token.Valid {
			continue
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			continue
		}

		if iss, _ := claims["iss"].(string); iss != issuer {
			continue
		}
		if aud, ok := claims["aud"].(string); !ok || aud != oidcmodels.ClientID {
			// aud can also be []interface{}
			if auds, ok := claims["aud"].([]any); ok {
				found := false
				for _, a := range auds {
					if a.(string) == oidcmodels.ClientID {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			} else {
				continue
			}
		}

		sub, _ := claims["sub"].(string)
		if sub == "" {
			continue
		}

		var person models.Person
		if err := ctx.DB().Where("id = ?", sub).First(&person).Error; err != nil {
			return false, nil
		}

		if err := InjectToken(ctx, c, &person, "oidc"); err != nil {
			return false, err
		}
		ctx = ctx.WithUser(&person)
		c.SetRequest(c.Request().WithContext(ctx))
		return true, nil
	}

	_ = lastErr
	return false, nil
}

func loadOIDCPublicKeys(ctx context.Context) ([]*rsa.PublicKey, error) {
	const cacheKey = "oidc_public_keys"
	if cached, ok := oidcPublicKeyCache.Get(cacheKey); ok {
		return cached.([]*rsa.PublicKey), nil
	}

	var dbKeys []oidcmodels.PublicKey
	if err := ctx.DB().Where("expires_at IS NULL OR expires_at > NOW()").Find(&dbKeys).Error; err != nil {
		return nil, err
	}

	keys := make([]*rsa.PublicKey, 0, len(dbKeys))
	for _, k := range dbKeys {
		pub, err := parseRSAPublicKeyPEM(k.PublicKey)
		if err != nil {
			continue
		}
		keys = append(keys, pub)
	}

	oidcPublicKeyCache.SetDefault(cacheKey, keys)
	return keys, nil
}

func parseRSAPublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaPub, nil
}
