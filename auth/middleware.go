package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
)

const (
	DefaultPostgrestRole = "postgrest_api"
)

var (
	IdentityRoleMapper string

	// identityMapperLoginCache is used to keep track of whether a new login session
	// has been checked for mapping to a team or not.
	identityMapperLoginCache = cache.New(1*time.Hour, 1*time.Hour)
)

type IdentityMapperExprResult struct {
	Teams []string `json:"teams"`
	Role  string   `json:"role"`
}

var (
	errInvalidTokenFormat = errors.New("invalid access token format")
	errTokenExpired       = errors.New("access token has expired")
	AuthMode              string
	Clerk                 = "clerk"
	Kratos                = "kratos"
)

var skipAuthPaths = []string{
	"/health",
	"/metrics",
	"/kratos/*",
	"/playbook/webhook/:webhook_path", // Playbook webhooks handle the authentication themselves
}

func Middleware(ctx context.Context, e *echo.Echo) error {
	if AuthMode == "" {
		logger.Errorf("authentication is disabled")
		return nil
	}
	var (
		adminUserID string
		err         error
	)

	switch AuthMode {
	case Kratos:
		kratosHandler := NewKratosHandler(KratosAPI, KratosAdminAPI, db.PostgRESTJWTSecret)
		adminUserID, err = kratosHandler.CreateAdminUser(ctx)
		if err != nil {
			return fmt.Errorf("Failed to created admin user: %v", err)
		}

		middleware, err := kratosHandler.KratosMiddleware(ctx)
		if err != nil {
			return fmt.Errorf("Failed to initialize kratos middleware: %v", err)
		}
		e.Use(middleware.Session)
		e.POST("/auth/invite_user", kratosHandler.InviteUser, rbac.Authorization(rbac.ObjectAuth, rbac.ActionWrite))

	case Clerk:
		if ClerkJWKSURL == "" {
			return fmt.Errorf("Failed to start server: clerk-jwks-url is required")
		}
		if ClerkOrgID == "" {
			return fmt.Errorf("Failed to start server: clerk-org-id is required")
		}

		clerkHandler, err := NewClerkHandler(ClerkJWKSURL, ClerkOrgID, db.PostgRESTJWTSecret)
		if err != nil {
			logger.Fatalf("Failed to initialize clerk client: %v", err)
		}
		e.Use(clerkHandler.Session)

		// We also need to disable "settings.users" feature in database
		// to hide the menu from UI
		if err := context.UpdateProperty(ctx, "settings.user.disabled", "true"); err != nil {
			return fmt.Errorf("Error setting property in database: %v", err)
		}

	default:
		return fmt.Errorf("Invalid auth provider: %s", AuthMode)
	}

	// Initiate RBAC
	if err := rbac.Init(ctx.DB(), adminUserID); err != nil {
		return fmt.Errorf("Failed to initialize rbac: %v", err)
	}
	return nil

}

func canSkipAuth(c echo.Context) bool {
	return collections.Contains(skipAuthPaths, c.Path())
}

func mapIDsToRoles(ctx context.Context, session *client.Session, uid uuid.UUID) error {
	if _, exists := identityMapperLoginCache.Get(session.GetId()); exists {
		return nil
	}

	env := map[string]any{
		"identity": session.Identity,
	}

	res, err := gomplate.RunTemplate(env, gomplate.Template{Expression: IdentityRoleMapper})
	if err != nil {
		return fmt.Errorf("error running IdentityRoleMapper template: %v", err)
	}

	if res == "" {
		return nil
	}

	var result IdentityMapperExprResult
	if err := json.Unmarshal([]byte(res), &result); err != nil {
		return err
	}

	if result.Role != "" {
		if _, err := rbac.Enforcer.AddRoleForUser(uid.String(), result.Role); err != nil {
			return fmt.Errorf("error adding role:%s to user %s: %v", result.Role, uid, err)
		}
	}

	for _, teamName := range result.Teams {
		team, err := duty.FindTeam(ctx, teamName)
		if err != nil {
			logger.Errorf("error finding team(name: %s) %v", team, err)
			continue
		}

		if err := db.AddPersonToTeam(ctx, uid, team.ID); err != nil {
			logger.Errorf("error adding person to team: %v", err)
		}
	}

	if session.ExpiresAt != nil {
		identityMapperLoginCache.Set(session.GetId(), nil, time.Until(*session.ExpiresAt))
	} else {
		identityMapperLoginCache.SetDefault(session.GetId(), nil)
	}
	return nil
}

// sessionCache is used to cache the response of a login attempt.
type sessionCache struct {
	session *client.Session
	err     error
}

func basicAuthCacheKey(username, separator, password string) string {
	return hash.Sha256Hex(fmt.Sprintf("%s:%s:%s", username, separator, password))
}
