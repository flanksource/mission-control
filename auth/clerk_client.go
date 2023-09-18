package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

const (
	clerkSessionCookie = "__session"
)

type ClerkHandler struct {
	dbJwtSecret string
	jwksURL     string
	orgID       string
	tokenCache  *cache.Cache
	userCache   *cache.Cache
}

func NewClerkHandler(jwksURL, orgID, dbJwtSecret string) (*ClerkHandler, error) {
	return &ClerkHandler{
		jwksURL:     jwksURL,
		orgID:       orgID,
		dbJwtSecret: dbJwtSecret,
		tokenCache:  cache.New(3*24*time.Hour, 12*time.Hour),
		userCache:   cache.New(3*24*time.Hour, 12*time.Hour),
	}, nil
}

func (h ClerkHandler) parseJWTToken(token string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	jt, err := jwt.ParseWithClaims(token, claims, getJWTKeyFunc(h.jwksURL))
	if !jt.Valid {
		return claims, fmt.Errorf("jwt token not valid")
	}
	return claims, err
}

func (h ClerkHandler) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) {
			return next(c)
		}

		// Extract session token from Authorization header
		sessionToken := c.Request().Header.Get(echo.HeaderAuthorization)
		sessionToken = strings.TrimSpace(strings.TrimPrefix(sessionToken, "Bearer "))
		if sessionToken == "" {
			// Check for `__session` cookie
			sessionTokenCookie, err := c.Request().Cookie(clerkSessionCookie)
			if err != nil {
				// Cookie not found
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}
			sessionToken = sessionTokenCookie.Value
		}

		if sessionToken == "" {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		ctx := c.(*api.Context)
		user, sessID, err := h.getUser(ctx, sessionToken)
		if err != nil {
			logger.Errorf("Error fetching user from clerk: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		token, err := getDBToken(h.tokenCache, h.dbJwtSecret, sessID, user.ID.String())
		if err != nil {
			logger.Errorf("Error generating JWT Token: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		c.Request().Header.Set(echo.HeaderAuthorization, fmt.Sprintf("Bearer %s", token))
		c.Request().Header.Set(api.UserIDHeaderKey, user.ID.String())

		ctx.WithUser(&api.ContextUser{ID: user.ID, Email: user.Email})
		return next(ctx)
	}
}

func (h *ClerkHandler) getUser(ctx *api.Context, sessionToken string) (*api.Person, string, error) {
	claims, err := h.parseJWTToken(sessionToken)
	if err != nil {
		return nil, "", err
	}
	sessionID := fmt.Sprint(claims["sid"])

	if user, exists := h.userCache.Get(sessionID); exists {
		return user.(*api.Person), sessionID, nil
	}

	if fmt.Sprint(claims["org_id"]) != h.orgID {
		return nil, "", fmt.Errorf("organization id does not match")
	}

	user := api.Person{
		Name:       fmt.Sprint(claims["name"]),
		Email:      fmt.Sprint(claims["email"]),
		Avatar:     fmt.Sprint(claims["image_url"]),
		ExternalID: fmt.Sprint(claims["user_id"]),
	}
	dbUser, err := h.createDBUserIfNotExists(ctx, user, fmt.Sprint(claims["role"]))
	if err != nil {
		return nil, "", err
	}
	h.userCache.SetDefault(sessionID, &dbUser)
	return &dbUser, sessionID, nil
}

func (h *ClerkHandler) createDBUserIfNotExists(ctx *api.Context, user api.Person, role string) (api.Person, error) {
	existingUser, err := db.GetUserByExternalID(ctx, user.ExternalID)
	if err == nil {
		// User with the given external ID exists
		return existingUser, nil
	}

	if err != gorm.ErrRecordNotFound {
		// Return if any other error, we only want to create the user
		return api.Person{}, err
	}

	dbUser, err := db.CreateUser(ctx, user)
	if err != nil {
		return api.Person{}, err
	}

	roleToAdd := rbac.RoleEditor
	if role == "admin" {
		roleToAdd = rbac.RoleAdmin
	}

	if _, err := rbac.Enforcer.AddRoleForUser(dbUser.ID.String(), roleToAdd); err != nil {
		return api.Person{}, err
	}

	return dbUser, nil
}