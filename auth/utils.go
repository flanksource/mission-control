package auth

import "github.com/golang-jwt/jwt/v4"

func generateDBToken(secret, id string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": DefaultPostgrestRole,
		"id":   id,
	})
	return token.SignedString([]byte(secret))
}
