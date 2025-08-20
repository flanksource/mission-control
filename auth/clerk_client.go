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

type AuthResult struct {
	User      *models.Person
	SessionID string
	Error     error
}

func (h ClerkHandler) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) {
			return next(c)
		}

		ctx := c.Request().Context().(context.Context)

		// Agents use basic auth with `token:<access_token>` format
		authResult := h.authenticateRequest(ctx, c)
		if err := authResult.Error; err != nil {
			// Check if response is set, then return that as error
			if c.Response().Committed {
				return err
			}
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}
		user, sessID := authResult.User, authResult.SessionID

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

func (h *ClerkHandler) getUserFromAccessToken(ctx context.Context, accessToken *models.AccessToken) AuthResult {
	sessionID := accessToken.ID.String()
	if user, exists := h.userCache.Get(sessionID); exists {
		return AuthResult{User: user.(*models.Person), SessionID: sessionID}
	}

	dbUser, err := db.GetUserByID(ctx, accessToken.PersonID.String())
	if err != nil {
		return AuthResult{Error: fmt.Errorf("error fetching user by id[%s]: %w", accessToken.PersonID, err)}
	}

	h.userCache.SetDefault(sessionID, &dbUser)
	return AuthResult{User: &dbUser, SessionID: sessionID}
}

func (h *ClerkHandler) getUserFromSessionToken(ctx context.Context, sessionToken string) AuthResult {
	claims, err := h.parseJWTToken(sessionToken)
	if err != nil {
		return AuthResult{Error: err}
	}
	sessionID := fmt.Sprint(claims["sid"])

	if user, exists := h.userCache.Get(sessionID); exists {
		return AuthResult{User: user.(*models.Person), SessionID: sessionID}
	}

	if fmt.Sprint(claims["org_id"]) != h.orgID {
		return AuthResult{Error: fmt.Errorf("organization id does not match")}
	}

	user := models.Person{
		Name:       fmt.Sprint(claims["name"]),
		Email:      fmt.Sprint(claims["email"]),
		Avatar:     fmt.Sprint(claims["image_url"]),
		ExternalID: fmt.Sprint(claims["user_id"]),
	}
	dbUser, err := h.createDBUserIfNotExists(ctx, user)
	if err != nil {
		return AuthResult{Error: err}
	}

	// If session expires, and clerk role is different from our rbac
	// we update the rbac
	if err := h.updateRole(dbUser.ID.String(), fmt.Sprint(claims["role"])); err != nil {
		return AuthResult{Error: err}
	}

	h.userCache.SetDefault(sessionID, &dbUser)
	return AuthResult{User: &dbUser, SessionID: sessionID}
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

// authenticateRequest handles all authentication methods and returns the authenticated user
func (h ClerkHandler) authenticateRequest(ctx context.Context, c echo.Context) AuthResult {
	// Try basic auth first
	if authResult := h.authenticateBasicAuth(ctx, c); authResult.Error != nil || authResult.User != nil {
		return authResult
	}

	// Try bearer token or cookie authentication
	return h.authenticateBearerOrCookie(ctx, c)
}

func (h ClerkHandler) authenticateBasicAuth(ctx context.Context, c echo.Context) AuthResult {
	username, password, ok := c.Request().BasicAuth()
	if !ok {
		return AuthResult{}
	}

	if strings.ToLower(username) != "token" {
		return AuthResult{Error: c.String(http.StatusUnauthorized, "Unauthorized: invalid username for basic auth")}
	}

	return h.authenticateWithToken(ctx, c, password)
}

// authenticateBearerOrCookie handles regular user authentication
func (h ClerkHandler) authenticateBearerOrCookie(ctx context.Context, c echo.Context) AuthResult {
	sessionToken := h.extractSessionToken(c)
	if sessionToken == "" {
		return AuthResult{Error: c.String(http.StatusUnauthorized, "Unauthorized")}
	}

	// Check if it's our custom token format (4 dots) or Clerk JWT (2 dots)
	if strings.Count(sessionToken, ".") == 4 {
		return h.authenticateWithToken(ctx, c, sessionToken)
	}

	// Standard Clerk JWT
	authResult := h.getUserFromSessionToken(ctx, sessionToken)
	if authResult.Error != nil {
		logger.Errorf("Error fetching user from clerk: %v", authResult.Error)
		return AuthResult{Error: c.String(http.StatusUnauthorized, "Unauthorized")}
	}

	return authResult
}

// authenticateWithToken handles authentication using our custom access token format
func (h ClerkHandler) authenticateWithToken(ctx context.Context, c echo.Context, token string) AuthResult {
	accessToken, err := getAccessToken(ctx, token)
	if err != nil {
		if errors.Is(err, errInvalidTokenFormat) || errors.Is(err, errTokenExpired) {
			ctx.GetSpan().RecordError(err)
			return AuthResult{Error: c.String(http.StatusUnauthorized, fmt.Sprintf("Unauthorized: %s", err.Error()))}
		}

		ctx.GetSpan().RecordError(fmt.Errorf("error fetching access_token: %w", err))
		return AuthResult{Error: c.String(http.StatusInternalServerError, "server error while fetching access token")}
	}

	if accessToken == nil {
		ctx.GetSpan().RecordError(fmt.Errorf("access token not found"))
		return AuthResult{Error: c.String(http.StatusUnauthorized, "Unauthorized: access token not found")}
	}

	authResult := h.getUserFromAccessToken(ctx, accessToken)
	if authResult.Error != nil {
		logger.Errorf("Error fetching user from access token: %v", authResult.Error)
		return AuthResult{Error: c.String(http.StatusUnauthorized, "Unauthorized")}
	}

	return authResult
}

// extractSessionToken retrieves the token from either the Authorization header or cookie
func (h ClerkHandler) extractSessionToken(c echo.Context) string {
	// Try Bearer Authorization header first
	if token, ok := extractBearerAuthToken(c.Request().Header); ok {
		return token
	}

	// Fall back to session cookie
	if cookie, err := c.Request().Cookie(clerkSessionCookie); err == nil {
		return cookie.Value
	}

	return ""
}
