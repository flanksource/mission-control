package auth

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/db"
	"github.com/golang-jwt/jwt/v4"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
)

func generateDBToken(secret, id string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": DefaultPostgrestRole,
		"id":   id,
	})
	return token.SignedString([]byte(secret))
}

func getDBToken(ctx context.Context, c *cache.Cache, dbJWTSecret, sessID, userID string) (string, error) {
	key := sessID + userID
	if token, exists := c.Get(key); exists {
		return token.(string), nil
	}
	// Adding Authorization Token for PostgREST
	token, err := generateDBToken(dbJWTSecret, userID)
	if err != nil {
		return "", err
	}

	if err := db.UpdateLastLogin(ctx, userID); err != nil {
		logger.Errorf("Error updating last login for user[%s]: %v", userID, err)
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

func getAccessToken(ctx context.Context, tokenCache *cache.Cache, token string) (*models.AccessToken, error) {
	if token, ok := tokenCache.Get(token); ok {
		return token.(*models.AccessToken), nil
	}

	fields := strings.Split(token, ".")
	if len(fields) != 5 {
		return nil, errInvalidTokenFormat
	}

	var (
		password = fields[0]
		salt     = fields[1]
	)

	timeCost, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return nil, errInvalidTokenFormat
	}

	memoryCost, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil {
		return nil, errInvalidTokenFormat
	}

	parallelism, err := strconv.ParseUint(fields[4], 10, 8)
	if err != nil {
		return nil, errInvalidTokenFormat
	}

	hash := argon2.IDKey([]byte(password), []byte(salt), uint32(timeCost), uint32(memoryCost), uint8(parallelism), 20)
	encodedHash := base64.URLEncoding.EncodeToString(hash)

	query := `SELECT access_tokens.* FROM access_tokens WHERE value = ?`
	var acessToken models.AccessToken
	if err := ctx.DB().Raw(query, encodedHash).First(&acessToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	if acessToken.ExpiresAt.Before(time.Now()) {
		return nil, errTokenExpired
	}

	tokenCache.Set(token, &acessToken, time.Until(acessToken.ExpiresAt))

	return &acessToken, nil
}
