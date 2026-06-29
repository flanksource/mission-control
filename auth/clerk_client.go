package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth/accesstoken"
	"github.com/flanksource/incident-commander/auth/oidc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/db"
)

const (
	clerkSessionCookie = "__session"
	clerkUserCacheTTL  = 5 * time.Minute
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
	jwks             *jwksCache
}

// jwksCache lazily fetches and caches the Clerk JWKS keyfunc. The underlying
// keyfunc.Get performs a network fetch and spawns a background-refresh goroutine,
// so it must be built once and shared rather than rebuilt per request. The cache
// is held behind a pointer so copies of ClerkHandler (value receivers) share the
// same instance and lock. A failed fetch is not cached, so the next request retries.
type jwksCache struct {
	url string
	mu  sync.Mutex
	fn  jwt.Keyfunc
}

func (c *jwksCache) keyfunc() (jwt.Keyfunc, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fn == nil {
		fn, err := newClerkKeyfunc(c.url)
		if err != nil {
			return nil, err
		}
		c.fn = fn
	}

	return c.fn, nil
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
		jwks:             &jwksCache{url: ClerkJwksUrl},
	}, nil
}

func (h ClerkHandler) parseJWTToken(token string) (jwt.MapClaims, error) {
	keyfunc, err := h.jwks.keyfunc()
	if err != nil {
		return nil, err
	}

	claims := jwt.MapClaims{}
	jt, err := jwt.ParseWithClaims(token, claims, keyfunc)
	if err != nil {
		return claims, err
	}
	if !jt.Valid {
		return claims, fmt.Errorf("jwt token not valid")
	}
	return claims, nil
}

type AuthResult struct {
	User        *models.Person
	AccessToken *models.AccessToken
	SessionID   string
}

func (h ClerkHandler) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) {
			return next(c)
		}

		ctx := c.Request().Context().(context.Context)

		if OIDCEnabled {
			if token, ok := extractBearerAuthToken(c.Request().Header); ok {
				if authenticated, err := authenticateOIDCToken(c, token); err != nil {
					return c.JSON(http.StatusInternalServerError, map[string]string{
						"error": err.Error(),
					})
				} else if authenticated {
					return next(c)
				}
			}
		}

		// Agents use basic auth with `token:<access_token>` format
		authResult, err := h.authenticateRequest(ctx, c)
		if err != nil {
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

func (h *ClerkHandler) getUserFromAccessToken(ctx context.Context, accessToken *models.AccessToken) (AuthResult, error) {
	sessionID := accessToken.ID.String()
	if user, exists := h.userCache.Get(sessionID); exists {
		return AuthResult{User: user.(*models.Person), SessionID: sessionID}, nil
	}

	dbUser, err := db.GetUserByID(ctx, accessToken.PersonID.String())
	if err != nil {
		return AuthResult{}, fmt.Errorf("error fetching user by id[%s]: %w", accessToken.PersonID, err)
	}

	h.userCache.SetDefault(sessionID, &dbUser)
	return AuthResult{User: &dbUser, SessionID: sessionID, AccessToken: accessToken}, nil
}

func (h *ClerkHandler) getUserFromSessionToken(ctx context.Context, sessionToken string) (AuthResult, error) {
	claims, err := h.parseJWTToken(sessionToken)
	if err != nil {
		return AuthResult{}, err
	}

	return h.getUserFromSessionClaims(ctx, claims)
}

func (h *ClerkHandler) getUserFromSessionClaims(ctx context.Context, claims jwt.MapClaims) (AuthResult, error) {
	orgID := clerkOrgID(claims)
	if orgID == "" {
		return AuthResult{}, dutyAPI.Errorf(dutyAPI.EINVALID, "missing Clerk organization id")
	}
	if orgID != h.orgID {
		return AuthResult{}, dutyAPI.Errorf(dutyAPI.EINVALID, "organization id does not match")
	}

	externalID := clerkUserID(claims)
	if externalID == "" {
		return AuthResult{}, dutyAPI.Errorf(dutyAPI.EINVALID, "missing Clerk user id")
	}

	sessionID := clerkClaimString(claims, "sid")
	cacheKey := clerkUserCacheKey(orgID, externalID)
	if user, exists := h.userCache.Get(cacheKey); exists {
		return AuthResult{User: user.(*models.Person), SessionID: sessionID}, nil
	}

	user := models.Person{
		Name:       clerkClaimString(claims, "name"),
		Email:      clerkClaimString(claims, "email"),
		Avatar:     clerkClaimString(claims, "image_url"),
		ExternalID: externalID,
	}
	dbUser, err := h.createDBUserIfNotExists(ctx, user)
	if err != nil {
		return AuthResult{}, err
	}

	// If session expires, and clerk role is different from our rbac
	// we update the rbac
	if err := h.updateRole(dbUser.ID.String(), clerkRole(claims)); err != nil {
		return AuthResult{}, ctx.Oops().Wrapf(err, "update Clerk role for user %s", dbUser.ID.String())
	}

	h.userCache.Set(cacheKey, &dbUser, clerkUserCacheTTL)
	return AuthResult{User: &dbUser, SessionID: sessionID}, nil
}

func clerkClaimString(claims jwt.MapClaims, key string) string {
	raw, ok := claims[key]
	if !ok || raw == nil {
		return ""
	}

	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func clerkClaimMap(claims jwt.MapClaims, key string) map[string]any {
	raw, ok := claims[key]
	if !ok || raw == nil {
		return nil
	}

	switch value := raw.(type) {
	case map[string]any:
		return value
	case jwt.MapClaims:
		return map[string]any(value)
	default:
		return nil
	}
}

func clerkMapString(claims map[string]any, key string) string {
	raw, ok := claims[key]
	if !ok || raw == nil {
		return ""
	}

	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func clerkOrgID(claims jwt.MapClaims) string {
	if orgID := clerkClaimString(claims, "org_id"); orgID != "" {
		return orgID
	}

	if org := clerkClaimMap(claims, "o"); org != nil {
		return clerkMapString(org, "id")
	}
	return ""
}

func clerkUserID(claims jwt.MapClaims) string {
	if userID := clerkClaimString(claims, "user_id"); userID != "" {
		return userID
	}
	return clerkClaimString(claims, "sub")
}

func clerkRole(claims jwt.MapClaims) string {
	if role := clerkClaimString(claims, "role"); role != "" {
		return role
	}
	if role := clerkClaimString(claims, "org_role"); role != "" {
		return role
	}
	if org := clerkClaimMap(claims, "o"); org != nil {
		return clerkMapString(org, "rol")
	}
	return ""
}

func clerkUserCacheKey(orgID, externalID string) string {
	return fmt.Sprintf("clerk:%s:%s", orgID, externalID)
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
func (h ClerkHandler) authenticateRequest(ctx context.Context, c echo.Context) (AuthResult, error) {
	// Try basic auth first
	if authResult, err := h.authenticateBasicAuth(ctx, c); err != nil || authResult.User != nil {
		return authResult, err
	}

	// Try bearer token or cookie authentication
	return h.authenticateBearerOrCookie(ctx, c)
}

func (h ClerkHandler) authenticateBasicAuth(ctx context.Context, c echo.Context) (AuthResult, error) {
	username, password, ok := c.Request().BasicAuth()
	if !ok {
		return AuthResult{}, nil
	}

	if strings.ToLower(username) != "token" {
		return AuthResult{}, c.String(http.StatusUnauthorized, "Unauthorized: invalid username for basic auth")
	}

	return h.authenticateWithToken(ctx, c, password)
}

// authenticateBearerOrCookie handles regular user authentication
func (h ClerkHandler) authenticateBearerOrCookie(ctx context.Context, c echo.Context) (AuthResult, error) {
	sessionToken := h.extractSessionToken(c)
	if sessionToken == "" {
		setWWWAuthenticate(c)
		return AuthResult{}, c.String(http.StatusUnauthorized, "Unauthorized")
	}

	// Check if it's our custom token format or Clerk JWT
	if _, err := accesstoken.Parse(sessionToken); err == nil {
		return h.authenticateWithToken(ctx, c, sessionToken)
	}

	// Standard Clerk JWT
	authResult, err := h.getUserFromSessionToken(ctx, sessionToken)
	if err != nil {
		logger.Errorf("Error fetching user from clerk: %v", err)
		return AuthResult{}, c.String(http.StatusUnauthorized, "Unauthorized")
	}

	return authResult, nil
}

// authenticateWithToken handles authentication using our custom access token format
func (h ClerkHandler) authenticateWithToken(ctx context.Context, c echo.Context, token string) (AuthResult, error) {
	accessToken, err := getAccessToken(ctx, token)
	if err != nil {
		if errors.Is(err, accesstoken.ErrInvalidFormat) || errors.Is(err, errTokenExpired) {
			ctx.GetSpan().RecordError(err)
			return AuthResult{}, c.String(http.StatusUnauthorized, fmt.Sprintf("Unauthorized: %s", err.Error()))
		}

		ctx.GetSpan().RecordError(fmt.Errorf("error fetching access_token: %w", err))
		return AuthResult{}, c.String(http.StatusInternalServerError, "server error while fetching access token")
	}

	if accessToken == nil {
		ctx.GetSpan().RecordError(fmt.Errorf("access token not found"))
		return AuthResult{}, c.String(http.StatusUnauthorized, "Unauthorized: access token not found")
	}

	authResult, err := h.getUserFromAccessToken(ctx, accessToken)
	if err != nil {
		logger.Errorf("Error fetching user from access token: %v", err)
		return AuthResult{}, c.String(http.StatusUnauthorized, "Unauthorized")
	}

	return authResult, nil
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

var _ oidc.ExternalLoginProvider = (*ClerkCredentialChecker)(nil)

// ClerkCredentialChecker validates Clerk browser sessions for the OIDC login flow.
type ClerkCredentialChecker struct {
	handler *ClerkHandler
}

func NewClerkCredentialChecker(h *ClerkHandler) *ClerkCredentialChecker {
	return &ClerkCredentialChecker{handler: h}
}

func (c *ClerkCredentialChecker) LoginRedirectURL(authRequestID string) (string, error) {
	frontendURL := strings.TrimRight(api.FrontendURL, "/")
	if frontendURL == "" {
		return "", fmt.Errorf("frontend URL is not configured")
	}

	returnTo := url.URL{Path: "/oidc/clerk/callback"}
	callbackQuery := returnTo.Query()
	callbackQuery.Set("auth_request_id", authRequestID)
	returnTo.RawQuery = callbackQuery.Encode()

	loginURL, err := url.Parse(frontendURL + "/login")
	if err != nil {
		return "", fmt.Errorf("invalid frontend URL: %w", err)
	}

	q := loginURL.Query()
	// Clerk login happens on the shared frontend, but this OIDC flow was started by the tenant backend.
	// The auth_request_id is stored in the backend's OIDC storage, so after the user signs in the
	// frontend callback relays the Clerk session token to the active org backend from Clerk metadata.
	q.Set("return_to", returnTo.String())
	loginURL.RawQuery = q.Encode()

	return loginURL.String(), nil
}

func (c *ClerkCredentialChecker) callbackSessionToken(ec echo.Context) string {
	if sessionToken := ec.Request().PostFormValue("clerk_session_token"); sessionToken != "" {
		return sessionToken
	}
	return c.handler.extractSessionToken(ec)
}

func (c *ClerkCredentialChecker) CallbackSubject(ec echo.Context) (string, error) {
	ctx := ec.Request().Context().(context.Context)

	sessionToken := c.callbackSessionToken(ec)
	if sessionToken == "" {
		return "", fmt.Errorf("no Clerk session token found")
	}
	if _, err := accesstoken.Parse(sessionToken); err == nil {
		return "", fmt.Errorf("access tokens cannot complete Clerk browser login")
	}

	authResult, err := c.handler.getUserFromSessionToken(ctx, sessionToken)
	if err != nil {
		return "", err
	}
	if authResult.User == nil {
		return "", fmt.Errorf("user is not authenticated")
	}

	return authResult.User.ID.String(), nil
}
