package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/vars"
	"github.com/golang-jwt/jwt/v4"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
)

func FlushTokenCache() {
	tokenCache.Flush()
}

// tokenCache caches
// - JWT for postgREST
// - RLS payloads
// - access tokens
var tokenCache = cache.New(1*time.Hour, 1*time.Hour)

func InjectToken(ctx context.Context, c echo.Context, user *models.Person, sessID string) error {
	token, err := GetOrCreateJWTToken(ctx, user, sessID)
	if err != nil {
		logger.Errorf("Error generating JWT Token: %v", err)
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	AddLoginContext(c, user)

	c.Request().Header.Set(echo.HeaderAuthorization, fmt.Sprintf("Bearer %s", token))
	return nil
}

// If a user only has these roles, then RLS must be enforced
var rlsEnforcableRoles = []string{
	policy.RoleEveryone,
	policy.RoleGuest,
	policy.RoleViewer,
}

func GetRLSPayload(ctx context.Context) (*rls.Payload, error) {
	if !ctx.Properties().On(false, vars.FlagRLSEnable) {
		return &rls.Payload{Disable: true}, nil
	}

	cacheKey := fmt.Sprintf("rls-payload-%s", ctx.User().ID.String())
	if cached, ok := tokenCache.Get(cacheKey); ok {
		return cached.(*rls.Payload), nil
	}

	if roles, err := rbac.RolesForUser(ctx.User().ID.String()); err != nil {
		return nil, err
	} else if extra, _ := lo.Difference(roles, rlsEnforcableRoles); len(extra) > 0 {
		payload := &rls.Payload{Disable: true}
		tokenCache.SetDefault(cacheKey, payload)
		return payload, nil
	}

	permissions, err := rbac.PermsForUser(ctx.User().ID.String())
	if err != nil {
		return nil, err
	}

	var permissionWithIDs []string
	for _, p := range permissions {
		if p.Action != policy.ActionRead && p.Action != "*" {
			continue
		}

		// TODO: support deny
		if p.Deny {
			continue
		}

		if uuid.Validate(p.ID) == nil {
			permissionWithIDs = append(permissionWithIDs, p.ID)
		}
	}

	var permModels []models.Permission
	if err := ctx.DB().Where("id IN ?", permissionWithIDs).Find(&permModels).Error; err != nil {
		return nil, fmt.Errorf("failed to get permission for ids: %w", err)
	}

	var (
		agentIDs []string
		tags     = []map[string]string{}
	)
	for _, p := range permModels {
		agentIDs = append(agentIDs, p.Agents...)
		if len(p.Tags) > 0 {
			tags = append(tags, p.Tags)
		}
	}

	payload := &rls.Payload{
		Agents: agentIDs,
		Tags:   tags,
	}
	tokenCache.SetDefault(cacheKey, payload)

	return payload, nil
}

func GetOrCreateJWTToken(ctx context.Context, user *models.Person, sessionId string) (string, error) {
	config := api.DefaultConfig
	key := sessionId + user.ID.String()

	if token, exists := tokenCache.Get(key); exists {
		return token.(string), nil
	}

	claims := jwt.MapClaims{
		"role": config.Postgrest.DBRole,
		"id":   user.ID.String(),
	}

	if rlsPayload, err := GetRLSPayload(ctx.WithUser(user)); err != nil {
		return "", ctx.Oops().Wrap(err)
	} else if rlsPayload.Disable {
		claims["disable_rls"] = true
	} else {
		if len(rlsPayload.Agents) > 0 {
			claims["agents"] = rlsPayload.Agents
		}

		if len(rlsPayload.Tags) > 0 {
			claims["tags"] = rlsPayload.Tags
		}
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(config.Postgrest.JWTSecret))
	if err != nil {
		return "", ctx.Oops().Wrap(err)
	}

	if err := db.UpdateLastLogin(ctx, user.ID.String()); err != nil {
		ctx.Errorf("Error updating last login for user[%s]: %v", user, err)
	}

	tokenCache.SetDefault(key, token)
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

func getAccessToken(ctx context.Context, token string) (*models.AccessToken, error) {
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
	var accessToken models.AccessToken
	if err := ctx.DB().Raw(query, encodedHash).First(&accessToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	if accessToken.ExpiresAt == nil {
		tokenCache.Set(token, &accessToken, -1)
	} else {
		if accessToken.ExpiresAt.Before(time.Now()) {
			return nil, errTokenExpired
		}

		tokenCache.Set(token, &accessToken, time.Until(*accessToken.ExpiresAt))
	}

	return &accessToken, nil
}
