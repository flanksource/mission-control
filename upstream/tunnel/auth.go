package tunnel

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/duty/upstream"
	"github.com/golang-jwt/jwt/v4"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/auth/signing"
)

const (
	UpstreamAuthHeader = "X-Flanksource-Upstream-JWT"
	upstreamSubject    = "mission-control-upstream"
	upstreamTokenTTL   = time.Minute
)

var ErrUpstreamJWKNotSet = errors.New("upstream JWK is not configured")

type UpstreamClaims struct {
	jwt.RegisteredClaims
}

func (c *UpstreamClaims) VerifyExpiresAt(cmp int64, req bool) bool {
	return c.RegisteredClaims.VerifyExpiresAt(time.Unix(cmp, 0), req)
}

func mintUpstreamToken() (string, error) {
	now := time.Now()
	claims := UpstreamClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    signing.Issuer,
			Subject:   upstreamSubject,
			Audience:  jwt.ClaimStrings{string(signing.AudienceAgent)},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(upstreamTokenTTL)),
		},
	}
	return signing.NewJWT(signing.AudienceAgent, &claims)
}

func verifyUpstreamToken(token, jwk string) error {
	if jwk == "" {
		return ErrUpstreamJWKNotSet
	}

	var claims UpstreamClaims
	if _, err := signing.ParseJWTWithJWK(token, &claims, signing.AudienceAgent, jwk); err != nil {
		return err
	}

	if claims.Subject != upstreamSubject {
		return fmt.Errorf("upstream JWT subject must be %q", upstreamSubject)
	}

	return nil
}

func authenticatedUpstreamHandler(config upstream.UpstreamConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(UpstreamAuthHeader)
		if token == "" {
			http.Error(w, "missing upstream auth token", http.StatusUnauthorized)
			return
		}
		r.Header.Del(UpstreamAuthHeader)

		if err := verifyUpstreamToken(token, config.JWK); err != nil {
			http.Error(w, "invalid upstream auth token", http.StatusUnauthorized)
			return
		}

		ctx := auth.WithTrustedUpstream(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
