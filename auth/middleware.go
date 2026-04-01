package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth/oidc"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
	slogecho "github.com/samber/slog-echo"

	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/vars"
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
	Clerk                 = "clerk"
	Kratos                = "kratos"
	Basic                 = "basic"
)

var skipAuthPathPrefixes = []string{
	"/kratos/",
	"/canary/webhook/",
	"/playbook/webhook/", // Playbook webhooks handle the authentication themselves
	"/auth/basic/",
	"/oidc/",
	"/.well-known/",
	"/oauth/", // Standard OIDC protocol endpoints (mounted at root to match the issuer URL).
}

var skipAuthPathsExact = []string{
	"/health",

	// --start:: Standard OIDC protocol endpoints (mounted at root to match the issuer URL).
	"/authorize",
	"/authorize/callback",
	"/userinfo",
	"/keys",
	"/revoke",
	"/device_authorization",
	"/endsession",
	// --end:: Standard OIDC endpoints
}

func Middleware(ctx context.Context, e *echo.Echo) error {
	if vars.AuthMode == "" && HtpasswdFile != "" {
		logger.Warnf("Htpasswd file is provided but auth mode is not set to 'basic'. Falling back to basic auth.")
		vars.AuthMode = Basic
	}
	if vars.AuthMode == "" {
		logger.Errorf("authentication is disabled")
		return nil
	}
	var (
		adminUserID string
		err         error
	)

	switch vars.AuthMode {
	case Basic:
		UseBasic(e)
		if admin, err := GetOrCreateAdminUser(ctx); err != nil {
			return fmt.Errorf("failed to created admin user: %v", err)
		} else if admin != nil {
			adminUserID = admin.ID.String()
		}
		if OIDCEnabled {
			htpasswdChecker, err := NewHtpasswdChecker(HtpasswdFile)
			if err != nil {
				return fmt.Errorf("failed to load htpasswd file: %w", err)
			}
			if err := oidc.MountRoutes(e, ctx, api.FrontendURL, OIDCSigningKeyPath, htpasswdChecker, LookupPersonByUsername); err != nil {
				return fmt.Errorf("failed to mount OIDC routes: %w", err)
			}
			logger.Infof("OIDC provider enabled at %s", api.FrontendURL)
		}
	case Kratos:
		kratosHandler := NewKratosHandler()
		adminUserID, err = kratosHandler.CreateAdminUser(ctx)
		if err != nil {
			return fmt.Errorf("failed to created admin user: %v", err)
		}

		kratosMiddleware, err := kratosHandler.KratosMiddleware(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize kratos middleware: %v", err)
		}
		e.Use(kratosMiddleware.Session)
		e.POST("/auth/invite_user", kratosHandler.InviteUser, rbac.Authorization(policy.ObjectAuth, policy.ActionUpdate))

		if OIDCEnabled {
			kratosChecker := NewKratosCredentialChecker(kratosMiddleware)
			if err := oidc.MountRoutes(e, ctx, api.FrontendURL, OIDCSigningKeyPath, kratosChecker, LookupKratosPersonByUsername); err != nil {
				return fmt.Errorf("failed to mount OIDC routes: %w", err)
			}
			logger.Infof("OIDC provider enabled at %s (Kratos auth)", api.FrontendURL)
		}

	case Clerk:
		clerkHandler, err := NewClerkHandler()
		if err != nil {
			logger.Fatalf("failed to initialize clerk client: %v", err)
		}
		e.Use(clerkHandler.Session)

		// We also need to disable "settings.users" feature in database
		// to hide the menu from UI
		if err := context.UpdateProperty(ctx, "settings.user.disabled", "true"); err != nil {
			return fmt.Errorf("error setting property in database: %v", err)
		}

	default:
		return fmt.Errorf("invalid auth provider: %s", vars.AuthMode)
	}

	// Initiate RBAC
	if err := dutyRBAC.Init(ctx, []string{adminUserID, api.SystemUserID.String()}, adapter.NewPermissionAdapter); err != nil {
		return fmt.Errorf("failed to initialize rbac: %w", err)
	}

	return nil
}

// TODO: Use regex supported path matching
func canSkipAuth(c echo.Context) bool {
	// use c.Request().URL.Path for exact matches instead of c.Path() which may contain path parameters (e.g. /playbook/webhook/:id)
	// Example: URL.PATH = /authorize/callback whereas c.Path() = /authorize/*
	if slices.Contains(skipAuthPathsExact, c.Request().URL.Path) {
		return true
	}

	for _, p := range skipAuthPathPrefixes {
		if strings.HasPrefix(c.Path(), p) {
			return true
		}
	}

	// /metrics requires auth by default, unless metrics.auth.disabled is true
	if c.Path() == "/metrics" && properties.On(false, "metrics.auth.disabled") {
		return true
	}

	return false
}

func mapIDsToRoles(ctx context.Context, session *client.Session, person models.Person) error {
	log := logger.GetLogger("auth")
	name := person.GetName()
	if _, exists := identityMapperLoginCache.Get(session.GetId()); exists {
		log.V(6).Infof("[%s] skipping identity mapping for session %s, already mapped", name, session.GetId())
		return nil
	}

	env := map[string]any{
		"identity": session.Identity,
	}

	if log.IsLevelEnabled(6) {
		log.V(6).Infof("[%s] mapping identity to roles/teams using %s", name, IdentityRoleMapper)
	} else if log.IsLevelEnabled(4) {
		log.V(4).Infof("[%s] mapping identity to roles/teams", name)
	}

	res, err := ctx.RunTemplate(gomplate.Template{Expression: IdentityRoleMapper}, env)
	if err != nil {
		return fmt.Errorf("error running IdentityRoleMapper template: %w", err)
	}

	log.V(3).Infof("[%s] identity mapper returned %s", name, res)

	if res == "" {
		return nil
	}

	var result IdentityMapperExprResult
	if err := json.Unmarshal([]byte(res), &result); err != nil {
		return ctx.Oops().
			With("result", res).
			Hint("https://docs.flanksource.com/reference/helm/mission-control/#identity-mapper").
			Wrapf(err, "identity role mapper did not produce a valid JSON encoded result")
	}

	if result.Role != "" {
		log.V(3).Infof("[%s] adding role: %s", name, res)
		if err := dutyRBAC.AddRoleForUser(person.ID.String(), result.Role); err != nil {
			return ctx.Oops().Wrapf(err, "error adding role %s to user %s", result.Role, name)
		}
	}

	for _, teamName := range result.Teams {
		team, err := query.FindTeam(ctx, teamName)
		if err != nil {
			log.Warnf("error finding team %s %v", team, err)
			continue
		}
		log.V(3).Infof("[%s] adding team: %s", name, team.Name)

		if err := db.AddPersonToTeam(ctx, person.ID, team.ID); err != nil {
			return ctx.Oops().Wrapf(err, "error adding team %s to user %s", team.ID, name)
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

func AddLoginContext(c echo.Context, person *models.Person) {
	if person == nil {
		return
	}
	slogecho.AddCustomAttributes(c, slog.String("user.name", person.Name))
	slogecho.AddCustomAttributes(c, slog.String("user.id", person.ID.String()))
}
