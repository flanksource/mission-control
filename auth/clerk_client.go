package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clerkinc/clerk-sdk-go/clerk"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
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

		var (
			user   *clerk.User
			err    error
			sessID string
		)
		user, sessID, err = h.getUser(sessionToken)
		if err != nil {
			logger.Errorf("Error fetching user from clerk: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		ctx := c.(*api.Context)
		if user.ExternalID == nil {
			user, err = h.createDBUser(ctx, user)
			if err != nil {
				logger.Errorf("Error creating user in database from clerk: %v", err)
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}
			// Clear user from cache so that new metadata is used
			h.userCache.Delete(sessID)
		}

		token, err := h.getDBToken(sessID, *user.ExternalID)
		if err != nil {
			logger.Errorf("Error generating JWT Token: %v", err)
		}

		c.Request().Header.Add(echo.HeaderAuthorization, fmt.Sprintf("Bearer %s", token))
		c.Request().Header.Add(UserIDHeaderKey, *user.ExternalID)
		return next(c)
	}
}

func (h *ClerkHandler) generateDBToken(id string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": DefaultPostgrestRole,
		"id":   id,
	})
	return token.SignedString([]byte(h.dbJwtSecret))
}

func (h *ClerkHandler) getDBToken(sessionID, userID string) (string, error) {
	cacheKey := sessionID + userID
	if token, exists := h.tokenCache.Get(cacheKey); exists {
		return token.(string), nil
	}
	// Adding Authorization Token for PostgREST
	token, err := h.generateDBToken(userID)
	if err != nil {
		return "", err
	}
	h.tokenCache.SetDefault(cacheKey, token)
	return token, nil
}

func (h *ClerkHandler) getUser(sessionToken string) (*clerk.User, string, error) {
	sessClaims, err := h.client.VerifyToken(sessionToken)
	if err != nil {
		return nil, "", err
	}

	cacheKey := sessClaims.SessionID
	if user, exists := h.userCache.Get(cacheKey); exists {
		return user.(*clerk.User), "", nil
	}

	user, err := h.client.Users().Read(sessClaims.Claims.Subject)
	if err != nil {
		return nil, "", err
	}
	h.userCache.SetDefault(cacheKey, user)
	return user, sessClaims.SessionID, nil
}

func (h *ClerkHandler) createDBUser(ctx *api.Context, user *clerk.User) (*clerk.User, error) {
	if user.ExternalID != nil {
		return user, nil
	}
	if user.PrimaryEmailAddressID == nil {
		return nil, fmt.Errorf("clerk.user[%s] has no primary email", user.ID)
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
	person := api.Person{
		Name:  strings.Join(name, " "),
		Email: email,
	}

	dbUser, err := db.GetOrCreateUser(ctx, person)
	if err != nil {
		return nil, err
	}

	id := dbUser.ID.String()
	userUpdateParams := clerk.UpdateUser{
		ExternalID: &id,
	}
	return h.client.Users().Update(user.ID, &userUpdateParams)
}
