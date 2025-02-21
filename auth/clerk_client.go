package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/db"
)

const (
	clerkSessionCookie = "__session"
)

var (
	ClerkJwksUrl string
	ClerkOrgID   string
)

type ClerkHandler struct {
	jwksURL          string
	orgID            string
	tokenCache       *cache.Cache
	accessTokenCache *cache.Cache
	userCache        *cache.Cache
}

func NewClerkHandler() (*ClerkHandler, error) {
	if ClerkJwksUrl == "" {
		return nil, fmt.Errorf("failed to start server: clerk-jwks-url is required")
	}
	if ClerkOrgID == "" {
		return nil, fmt.Errorf("failed to start server: clerk-org-id is required")
	}

	return &ClerkHandler{
		jwksURL:          ClerkJwksUrl,
		orgID:            ClerkOrgID,
		tokenCache:       cache.New(3*24*time.Hour, 12*time.Hour),
		accessTokenCache: cache.New(3*24*time.Hour, 12*time.Hour),
		userCache:        cache.New(3*24*time.Hour, 12*time.Hour),
	}, nil
}

func (h ClerkHandler) parseJWTToken(token string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	jt, err := jwt.ParseWithClaims(token, claims, getJWTKeyFunc(h.jwksURL))
	if err != nil {
		return claims, err
	}
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

		ctx := c.Request().Context().(context.Context)

		var (
			user   *models.Person
			sessID string
			err    error
		)

		// Agents use basic auth with `token:<access_token>` format
		if username, password, ok := c.Request().BasicAuth(); ok {
			if strings.ToLower(username) != "token" {
				return c.String(http.StatusUnauthorized, "Unauthorized: invalid username for basic auth")
			}
			accessToken, err := getAccessToken(ctx, password)
			if err != nil {
				if errors.Is(err, errInvalidTokenFormat) || errors.Is(err, errTokenExpired) {
					ctx.GetSpan().RecordError(err)
					return c.String(http.StatusUnauthorized, fmt.Sprintf("Unauthorized: %s", err.Error()))
				}
				ctx.GetSpan().RecordError(fmt.Errorf("error fetching access_token: %w", err))
				return c.String(http.StatusInternalServerError, "server error while fetching access token")
			}
			if accessToken == nil {
				ctx.GetSpan().RecordError(fmt.Errorf("access token not found"))
				return c.String(http.StatusUnauthorized, "Unauthorized: access token not found")
			}

			user, sessID, err = h.getUserFromAccessToken(ctx, accessToken)
			if err != nil {
				logger.Errorf("Error fetching user from access token: %v", err)
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

		} else {
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

			user, sessID, err = h.getUserFromSessionToken(ctx, sessionToken)
			if err != nil {
				logger.Errorf("Error fetching user from clerk: %v", err)
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}
		}

		// This is a catch-all if user is unset
		// In normal scenarios either the user will be set via session or token
		// and errors in those flows should be handled earlier.
		// This condition should never be met, if it is, there is an implementation problem
		if user == nil {
			return c.String(http.StatusUnauthorized, "Unable to authenticate user")
		}

		if err := InjectToken(ctx, c, user, sessID); err != nil {
			return err
		}

		ctx.GetSpan().SetAttributes(
			attribute.String("clerk-user-id", user.ExternalID),
			attribute.String("clerk-org-id", h.orgID),
		)

		ctx = ctx.WithUser(user)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func (h *ClerkHandler) getUserFromAccessToken(ctx context.Context, accessToken *models.AccessToken) (*models.Person, string, error) {
	sessionID := accessToken.ID.String()
	if user, exists := h.userCache.Get(sessionID); exists {
		return user.(*models.Person), sessionID, nil
	}

	dbUser, err := db.GetUserByID(ctx, accessToken.PersonID.String())
	if err != nil {
		return nil, "", fmt.Errorf("error fetching user by id[%s]: %w", accessToken.PersonID, err)
	}

	h.userCache.SetDefault(sessionID, &dbUser)
	return &dbUser, sessionID, nil
}

func (h *ClerkHandler) getUserFromSessionToken(ctx context.Context, sessionToken string) (*models.Person, string, error) {
	claims, err := h.parseJWTToken(sessionToken)
	if err != nil {
		return nil, "", err
	}
	sessionID := fmt.Sprint(claims["sid"])

	if user, exists := h.userCache.Get(sessionID); exists {
		return user.(*models.Person), sessionID, nil
	}

	if fmt.Sprint(claims["org_id"]) != h.orgID {
		return nil, "", fmt.Errorf("organization id does not match")
	}

	user := models.Person{
		Name:       fmt.Sprint(claims["name"]),
		Email:      fmt.Sprint(claims["email"]),
		Avatar:     fmt.Sprint(claims["image_url"]),
		ExternalID: fmt.Sprint(claims["user_id"]),
	}
	dbUser, err := h.createDBUserIfNotExists(ctx, user)
	if err != nil {
		return nil, "", err
	}

	// If session expires, and clerk role is different from our rbac
	// we update the rbac
	if err := h.updateRole(dbUser.ID.String(), fmt.Sprint(claims["role"])); err != nil {
		return nil, "", err
	}

	h.userCache.SetDefault(sessionID, &dbUser)
	return &dbUser, sessionID, nil
}

func (h *ClerkHandler) createDBUserIfNotExists(ctx context.Context, user models.Person) (models.Person, error) {
	existingUser, err := db.GetUserByExternalID(ctx, user.ExternalID)
	if err == nil {
		// User with the given external ID exists
		return existingUser, nil
	}

	if err != gorm.ErrRecordNotFound {
		// Return if any other error, we only want to create the user
		return models.Person{}, err
	}

	dbUser, err := db.CreateUser(ctx, user)
	if err != nil {
		return models.Person{}, err
	}

	return dbUser, nil
}

func (ClerkHandler) updateRole(userID, clerkRole string) error {
	// Clerk roles map to one of these 3 roles
	roles := []string{policy.RoleAdmin, policy.RoleViewer, policy.RoleGuest}

	var role string
	switch clerkRole {
	case "admin":
		role = policy.RoleAdmin
	case "org:guest":
		role = policy.RoleGuest
	default:
		role = policy.RoleViewer
	}

	if err := rbac.AddRoleForUser(userID, role); err != nil {
		return fmt.Errorf("failed to add role %s to user %s: %w", role, userID, err)
	}

	// Delete other roles
	for _, r := range roles {
		if r != role {
			if err := rbac.DeleteRoleForUser(userID, r); err != nil {
				return fmt.Errorf("failed to delete role %s: %w", r, err)
			}
		}
	}

	return nil
}
