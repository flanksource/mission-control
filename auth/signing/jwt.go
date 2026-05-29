package signing

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v4"
)

type Audience string

type Claims interface {
	jwt.Claims
	VerifyAudience(cmp string, req bool) bool
	VerifyIssuer(cmp string, req bool) bool
	VerifyExpiresAt(cmp int64, req bool) bool
}

const (
	Issuer         = "mission-control"
	MaxJWTValidity = 7 * 24 * time.Hour

	// Audience is postgREST server
	AudiencePostgREST Audience = "mission-control-postgrest"

	// Audience is the mission-control auth middleware when operating in basic auth mode
	AudienceBasicAuth Audience = "mission-control-basic-auth"

	// Audience is the plugin host.
	// Issue to a plugin during operation invocation so the plugin can use it back to authenticate itself.
	AudiencePluginInvocation Audience = "mission-control-plugin-host"

	// Audience is the mission-control agent.
	// Used by upstream to identify itself when communicating over the yamux stream.
	AudienceAgent Audience = "mission-control-agent"
)

var validAudiences = map[Audience]struct{}{
	AudiencePostgREST:        {},
	AudienceBasicAuth:        {},
	AudiencePluginInvocation: {},
	AudienceAgent:            {},
}

func (a Audience) Valid() error {
	if _, ok := validAudiences[a]; !ok {
		return fmt.Errorf("invalid JWT audience %q", a)
	}

	return nil
}

// NewJWT signs claims with the initialized RSA private key and sets the key ID
// header used by verifiers such as PostgREST.
func NewJWT(audience Audience, claims Claims) (string, error) {
	if err := validateClaims(audience, claims); err != nil {
		return "", err
	}

	privateKey, keyID, err := PrivateKey()
	if err != nil {
		return "", err
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID
	return token.SignedString(privateKey)
}

// ParseJWT verifies a Mission Control-issued JWT using the initialized signing
// public key and validates the standard Mission Control claims for the expected
// audience.
func ParseJWT(tokenString string, claims Claims, audience Audience) (*jwt.Token, error) {
	pub, _, err := PublicKey()
	if err != nil {
		return nil, err
	}

	return parseJWTWithKeyfunc(tokenString, claims, audience, RSAKeyfunc(pub))
}

// ParseJWTWithJWK verifies a Mission Control-issued JWT using the supplied
// public JWK JSON and validates the standard Mission Control claims for the
// expected audience.
func ParseJWTWithJWK(tokenString string, claims Claims, audience Audience, jwkJSON string) (*jwt.Token, error) {
	var jwk jose.JSONWebKey
	if err := json.Unmarshal([]byte(jwkJSON), &jwk); err != nil {
		return nil, fmt.Errorf("parse JWK: %w", err)
	}

	return parseJWTWithKeyfunc(tokenString, claims, audience, JWKKeyfunc(jwk))
}

func parseJWTWithKeyfunc(tokenString string, claims Claims, audience Audience, keyfunc jwt.Keyfunc) (*jwt.Token, error) {
	token, err := jwt.ParseWithClaims(tokenString, claims, keyfunc)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("JWT is invalid")
	}
	if err := validateClaims(audience, claims); err != nil {
		return nil, err
	}

	return token, nil
}

func validateClaims(audience Audience, claims Claims) error {
	if err := audience.Valid(); err != nil {
		return err
	}
	if !claims.VerifyAudience(string(audience), true) {
		return fmt.Errorf("JWT claims missing expected audience %q", audience)
	}
	if !claims.VerifyIssuer(Issuer, true) {
		return fmt.Errorf("JWT claims missing expected issuer %q", Issuer)
	}
	now := time.Now()
	if !claims.VerifyExpiresAt(now.Unix(), true) {
		return fmt.Errorf("JWT claims missing or expired exp")
	}
	if claims.VerifyExpiresAt(now.Add(MaxJWTValidity).Add(time.Second).Unix(), true) {
		return fmt.Errorf("JWT exp exceeds maximum validity of %s", MaxJWTValidity)
	}
	return nil
}

// RSAKeyfunc returns the public key from the provided RSA public key
func RSAKeyfunc(pub *rsa.PublicKey) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: got %v, expected RSA", token.Header["alg"])
		}
		return pub, nil
	}
}

// JWKKeyfunc returns the public key from the provide JWK
func JWKKeyfunc(jwk jose.JSONWebKey) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: got %v, expected RSA", token.Header["alg"])
		}

		if kid, _ := token.Header["kid"].(string); kid != "" && jwk.KeyID != "" && kid != jwk.KeyID {
			return nil, fmt.Errorf("JWK kid %q does not match token kid %q", jwk.KeyID, kid)
		}

		pub, ok := jwk.Key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("JWK does not contain an RSA public key")
		}

		return pub, nil
	}
}
