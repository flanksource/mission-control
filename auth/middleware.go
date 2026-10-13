package auth

import (
	gocontext "context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth/oidc"
	pluginapi "github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/shipped"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	slogecho "github.com/samber/slog-echo"

	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/vars"
)

var (
	errTokenExpired = errors.New("access token has expired")
	Clerk           = "clerk"
	Kratos          = "kratos"
	Basic           = "basic"
)

var skipAuthPathPrefixes = []string{
	"/kratos/",
	"/canary/webhook/",
	"/playbook/webhook/", // Playbook webhooks handle the authentication themselves
	"/auth/basic/",
	"/auth/kratos/error",
	"/auth/kratos/hooks/",
	"/oidc/",
	"/.well-known/",
	"/oauth/", // Standard OIDC protocol endpoints (mounted at root to match the issuer URL).
}

var skipAuthPathsExact = []string{
	"/health",

	// Basic auth endpoints — they handle credentials themselves.
	"/auth/login",
	"/auth/logout",

	// --start:: Standard OIDC protocol endpoints (mounted at root to match the issuer URL).
	"/authorize",
	"/authorize/callback",
	"/userinfo",
	"/keys",
	"/revoke",
	"/device_authorization",
	"/end_session",
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

		// joinsAdmins is set when admin@local is the first admin, i.e. in the basic and kratos auth modes
		joinsAdmins bool
	)

	switch vars.AuthMode {
	case Basic:
		e.Use(withFederatedAuth(UseBasic(e)))
		if admin, err := GetOrCreateAdminUser(ctx); err != nil {
			return fmt.Errorf("failed to created admin user: %v", err)
		} else if admin != nil {
			adminUserID = admin.ID.String()
			joinsAdmins = true
		}
		if OIDCEnabled {
			if localhostOnly {
				logger.Warnf("OIDC provider disabled because htpasswd file is not available")
			} else {
				htpasswdChecker, err := NewHtpasswdChecker(HtpasswdFile)
				if err != nil {
					return fmt.Errorf("failed to load htpasswd file: %w", err)
				}
				if err := oidc.MountRoutes(e, ctx, OIDCIssuerURL(), htpasswdChecker, nil, LookupPersonByUsername); err != nil {
					return fmt.Errorf("failed to mount OIDC routes: %w", err)
				}
				logger.Infof("OIDC provider enabled at %s", OIDCIssuerURL())
			}
		}
	case Kratos:
		kratosHandler := NewKratosHandler()
		adminUserID, err = kratosHandler.CreateAdminUser(ctx)
		if err != nil {
			return fmt.Errorf("failed to created admin user: %v", err)
		}
		joinsAdmins = true

		kratosMiddleware, err := kratosHandler.KratosMiddleware(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize kratos middleware: %v", err)
		}
		e.Use(withFederatedAuth(kratosMiddleware.Session))
		RegisterKratosRoutes(e, kratosHandler)

		if OIDCEnabled {
			kratosChecker := NewKratosCredentialChecker(kratosMiddleware)
			if err := oidc.MountRoutes(e, ctx, OIDCIssuerURL(), nil, kratosChecker, nil); err != nil {
				return fmt.Errorf("failed to mount OIDC routes: %w", err)
			}
			logger.Infof("OIDC provider enabled at %s (Kratos auth)", OIDCIssuerURL())
		}

	case Clerk:
		clerkHandler, err := NewClerkHandler()
		if err != nil {
			logger.Fatalf("failed to initialize clerk client: %v", err)
		}
		e.Use(withFederatedAuth(clerkHandler.Session))

		if OIDCEnabled {
			clerkChecker := NewClerkCredentialChecker(clerkHandler)
			if err := oidc.MountRoutes(e, ctx, OIDCIssuerURL(), nil, clerkChecker, nil); err != nil {
				return fmt.Errorf("failed to mount OIDC routes: %w", err)
			}
			logger.Infof("OIDC provider enabled at %s (Clerk auth)", OIDCIssuerURL())
		}

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

	// After RBAC is initialised, so admin@local's team membership reaches the policy
	if joinsAdmins {
		joinAdmins(ctx, adminUserID)
	}

	return nil
}

// RegisterKratosRoutes registers the invite and registration routes of the Kratos auth mode.
func RegisterKratosRoutes(e *echo.Echo, k *KratosHandler) {
	e.POST("/auth/invite_user", k.InviteUser, rbac.PersonManage(policy.ObjectAuth, policy.ActionUpdate))
	e.GET("/auth/kratos/error", KratosErrorRedirect)
	e.POST("/auth/kratos/hooks/registration/before", k.BeforeRegistrationWebhook)
	e.POST("/auth/kratos/hooks/registration/after", k.AfterRegistrationWebhook)
}

// joinAdmins adds admin@local to the shipped admins team, which is bound to the shipped admin Role.
// It's logged rather than returned: failing to start would lock everyone out, while admin@local keeps its
// built-in admin role and the next start tries again.
func joinAdmins(ctx context.Context, adminUserID string) {
	adminID, err := uuid.Parse(adminUserID)
	if err == nil {
		err = shipped.AddToAdmins(ctx, adminID)
	}
	if err != nil {
		logger.Errorf("failed to add %s to the %s team: %v", AdminEmail, shipped.TeamAdmins, err)
	}
}

type trustedUpstreamContextKey struct{}

func WithTrustedUpstream(ctx gocontext.Context) gocontext.Context {
	return gocontext.WithValue(ctx, trustedUpstreamContextKey{}, true)
}

func IsTrustedUpstream(ctx gocontext.Context) bool {
	v, _ := ctx.Value(trustedUpstreamContextKey{}).(bool)
	return v
}

// TODO: Use regex supported path matching
func canSkipAuth(c echo.Context) bool {
	if IsTrustedUpstream(c.Request().Context()) {
		// Incoming requests from upstream have already been authenticated by the agent tunnel.
		return true
	}

	requestPath := c.Request().URL.Path
	// Use the request path instead of c.Path(), which can be empty or contain route
	// patterns while global middleware is evaluating a request.
	if slices.Contains(skipAuthPathsExact, requestPath) {
		return true
	}

	for _, p := range skipAuthPathPrefixes {
		if strings.HasPrefix(requestPath, p) {
			return true
		}
	}

	// Plugin gateway requests that authenticate themselves (a plugin UI bundle,
	// or an operation call carrying a plugin invocation token) bypass the user
	// session so a whitelisted plugin's UI can be embedded cross-site.
	if pluginSelfAuthenticatedRequest(c) {
		return true
	}

	// /metrics requires auth by default, unless metrics.auth.disabled is true
	if requestPath == "/metrics" && properties.On(false, "metrics.auth.disabled") {
		return true
	}

	return false
}

// pluginSelfAuthenticatedRequest reports whether a plugin gateway request carries
// its own authentication instead of relying on the user's session cookie. It is
// strictly scoped to /api/plugins/ so it can never bypass auth for another route:
//
//   - plugin UI static assets are public for registered plugins (the gateway
//     404s unregistered ones); the bundle is shipped in the plugin and carries no
//     user data.
//   - operation/invoke calls carry a plugin invocation token in a header, which
//     the gateway validates before serving.
//
// This lets a whitelisted plugin's UI run in a cross-site iframe, where the
// SameSite session cookie is never sent.
func pluginSelfAuthenticatedRequest(c echo.Context) bool {
	requestPath := c.Request().URL.Path
	if !strings.HasPrefix(requestPath, pluginAPIPathPrefix) {
		return false
	}
	if isPluginUIPath(requestPath) {
		return true
	}
	return c.Request().Header.Get(pluginapi.InvocationTokenHTTPHeader) != ""
}

func isPluginUIPath(requestPath string) bool {
	rest := strings.TrimPrefix(requestPath, pluginAPIPathPrefix)
	_, tail, found := strings.Cut(rest, "/")
	if !found {
		return false
	}
	return tail == "ui" || strings.HasPrefix(tail, "ui/")
}

const pluginAPIPathPrefix = "/api/plugins/"

// mapIDsToRoles runs the Kratos team mapper on the person's identity. It runs again on a new session,
// i.e. after they sign in, and whenever their identity changes.
func mapIDsToRoles(ctx context.Context, session *client.Session, person models.Person) error {
	input := map[string]any{"session": session.GetId(), "identity": session.Identity}
	env := map[string]any{"identity": session.Identity}
	return mapTeams(ctx, person.ID, IdentityRoleMapper, env, input)
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
