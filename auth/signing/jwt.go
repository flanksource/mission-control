package signing

import (
	"crypto/rsa"
	"fmt"

	"github.com/golang-jwt/jwt/v4"
)

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
