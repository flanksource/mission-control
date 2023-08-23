package auth

import (
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/flanksource/commons/logger"
	"github.com/golang-jwt/jwt/v4"
	"github.com/patrickmn/go-cache"
)

func generateDBToken(secret, id string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": DefaultPostgrestRole,
		"id":   id,
	})
	return token.SignedString([]byte(secret))
}

func getDBToken(c *cache.Cache, dbJWTSecret, sessID, userID string) (string, error) {
	key := sessID + userID
	if token, exists := c.Get(key); exists {
		return token.(string), nil
	}
	// Adding Authorization Token for PostgREST
	token, err := generateDBToken(dbJWTSecret, userID)
	if err != nil {
		return "", err
	}
	c.SetDefault(key, token)
	return token, nil
}

func getJWTKeyFunc(jwksURL string) jwt.Keyfunc {
	// Create the keyfunc options. Use an error handler that logs. Refresh the JWKS when a JWT signed by an unknown KID
	// is found or at the specified interval. Rate limit these refreshes. Timeout the initial JWKS refresh request after
	// 10 seconds. This timeout is also used to create the initial context.Context for keyfunc.Get.
	options := keyfunc.Options{
		RefreshErrorHandler: func(err error) {
			logger.Errorf("There was an error with the jwt.Keyfunc\nError: %s", err.Error())
		},
		RefreshInterval:   time.Hour,
		RefreshRateLimit:  time.Minute * 5,
		RefreshTimeout:    time.Second * 10,
		RefreshUnknownKID: true,
	}

	// Create the JWKS from the resource at the given URL.
	jwks, err := keyfunc.Get(jwksURL, options)
	if err != nil {
		logger.Fatalf("Failed to create JWKS from resource at the given URL.\nError: %s", err.Error())
		// TODO Handle
	}
	return jwks.Keyfunc
}
