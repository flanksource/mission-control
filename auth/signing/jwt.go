package signing

import (
	"crypto/rsa"
	"fmt"
	"time"

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

	AudiencePostgREST        Audience = "mission-control-postgrest"
	AudienceBasicAuth        Audience = "mission-control-basic-auth"
	AudiencePluginInvocation Audience = "mission-control-plugin-host"
)

var validAudiences = map[Audience]struct{}{
	AudiencePostgREST:        {},
	AudienceBasicAuth:        {},
	AudiencePluginInvocation: {},
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

func ParseJWT(tokenString string, claims Claims, audience Audience) (*jwt.Token, error) {
	pub, _, err := PublicKey()
	if err != nil {
		return nil, err
	}

	token, err := jwt.ParseWithClaims(tokenString, claims, RSAKeyfunc(pub))
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

// RSAKeyfunc returns a jwt.Keyfunc that only accepts RSA signing methods and
// verifies tokens with the provided public key.
func RSAKeyfunc(pub *rsa.PublicKey) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: got %v, expected RSA", token.Header["alg"])
		}
		return pub, nil
	}
}
