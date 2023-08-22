package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clerkinc/clerk-sdk-go/clerk"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

const (
	clerkSessionCookie = "__session"
)

type ClerkHandler struct {
	client      clerk.Client
	dbJwtSecret string
	tokenCache  *cache.Cache
	userCache   *cache.Cache
}

func NewClerkHandler(secretKey, dbJwtSecret string) (*ClerkHandler, error) {
	client, err := clerk.NewClient(secretKey)
	if err != nil {
		return nil, err
	}

	return &ClerkHandler{
		client:      client,
		dbJwtSecret: dbJwtSecret,
		tokenCache:  cache.New(3*24*time.Hour, 12*time.Hour),
		userCache:   cache.New(3*24*time.Hour, 12*time.Hour),
	}, nil
}

func (h ClerkHandler) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) {
			return next(c)
		}

		// Extract session token from Authorization header
		sessionToken := c.Request().Header.Get(echo.HeaderAuthorization)
		sessionToken = strings.TrimPrefix(sessionToken, "Bearer ")
		if sessionToken == "" {
			// Check for `__session` cookie
			sessionTokenCookie, err := c.Request().Cookie(clerkSessionCookie)
			if err != nil {
				// Cookie not found
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}
			sessionToken = sessionTokenCookie.Value
		}

		ctx := c.(*api.Context)
		user, sessID, err := h.getUser(ctx, sessionToken)
		if err != nil {
			logger.Errorf("Error fetching user from clerk: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		token, err := h.getDBToken(sessID, user.ID.String())
		if err != nil {
			logger.Errorf("Error generating JWT Token: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		c.Request().Header.Set(echo.HeaderAuthorization, fmt.Sprintf("Bearer %s", token))
		c.Request().Header.Set(UserIDHeaderKey, user.ID.String())

		ctx.Context = context.WithValue(ctx.Context, api.UserIDContextKey, user.ID.String())
		return next(ctx)
	}
}

func (h *ClerkHandler) getDBToken(sessionID, userID string) (string, error) {
	cacheKey := sessionID + userID
	if token, exists := h.tokenCache.Get(cacheKey); exists {
		return token.(string), nil
	}
	// Adding Authorization Token for PostgREST
	token, err := generateDBToken(h.dbJwtSecret, userID)
	if err != nil {
		return "", err
	}
	h.tokenCache.SetDefault(cacheKey, token)
	return token, nil
}

func (h *ClerkHandler) getUser(ctx *api.Context, sessionToken string) (*api.Person, string, error) {
	sessClaims, err := h.client.VerifyToken(sessionToken)
	if err != nil {
		return nil, "", err
	}

	cacheKey := sessClaims.SessionID
	if user, exists := h.userCache.Get(cacheKey); exists {
		return user.(*api.Person), sessClaims.SessionID, nil
	}

	clerkUser, err := h.client.Users().Read(sessClaims.Claims.Subject)
	if err != nil {
		return nil, "", err
	}

	dbUser, err := h.createDBUserIfNotExists(ctx, clerkUser)
	if err != nil {
		return nil, "", err
	}
	h.userCache.SetDefault(cacheKey, &dbUser)
	return &dbUser, sessClaims.SessionID, nil
}

func (h *ClerkHandler) createDBUserIfNotExists(ctx *api.Context, user *clerk.User) (api.Person, error) {
	existingUser, err := db.GetUserByExternalID(ctx, user.ID)
	if err == nil {
		// User with the given clerk ID exists
		return existingUser, nil
	}

	if err != gorm.ErrRecordNotFound {
		// Return if any other error, we only want to create the user
		return api.Person{}, err
	}

	if user.PrimaryEmailAddressID == nil {
		return api.Person{}, fmt.Errorf("clerk.user[%s] has no primary email", user.ID)
	}

	var email string
	for _, addr := range user.EmailAddresses {
		if addr.ID == *user.PrimaryEmailAddressID {
			email = addr.EmailAddress
			break
		}
	}

	var name []string
	if user.FirstName != nil {
		name = append(name, *user.FirstName)
	}
	if user.LastName != nil {
		name = append(name, *user.LastName)
	}

	newUser := api.Person{
		Name:       strings.Join(name, " "),
		Email:      email,
		ExternalID: user.ID,
	}
	return db.CreateUser(ctx, newUser)
}
