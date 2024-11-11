package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/vars"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
	slogecho "github.com/samber/slog-echo"
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

var skipAuthPaths = []string{
	"/health",
	"/metrics",
	"/kratos/",
	"/canary/webhook/",
	"/playbook/webhook/", // Playbook webhooks handle the authentication themselves
}

func Middleware(ctx context.Context, e *echo.Echo) error {
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
	case Kratos:
		kratosHandler := NewKratosHandler()
		adminUserID, err = kratosHandler.CreateAdminUser(ctx)
		if err != nil {
			return fmt.Errorf("failed to created admin user: %v", err)
		}

		middleware, err := kratosHandler.KratosMiddleware(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize kratos middleware: %v", err)
		}
		e.Use(middleware.Session)
		e.POST("/auth/invite_user", kratosHandler.InviteUser, rbac.Authorization(rbac.ObjectAuth, rbac.ActionUpdate))

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
	if err := rbac.Init(ctx, adminUserID); err != nil {
		return fmt.Errorf("failed to initialize rbac: %v", err)
	}

	return nil
}

// TODO: Use regex supported path matching
func canSkipAuth(c echo.Context) bool {
	for _, p := range skipAuthPaths {
		if strings.HasPrefix(c.Path(), p) {
			return true
		}
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
			With("result", result).
			Hint("https://docs.flanksource.com/reference/helm/mission-control/#identity-mapper").
			Wrapf(err, "identity role mapper did not produce a valid JSON encoded result")
	}

	if result.Role != "" {
		log.V(3).Infof("[%s] adding role: %s", name, res)
		if err := rbac.AddRoleForUser(person.ID.String(), result.Role); err != nil {
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
