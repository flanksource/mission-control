package rbac

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

var (
	ErrNoUserID          = errors.New("unauthorized. User not found for RBAC")
	ErrAccessDenied      = errors.New("unauthorized. Access Denied")
	ErrMisconfiguredRBAC = errors.New("unauthorized. RBAC policy not configured correctly")
)

type MiddlewareFunc = func(echo.HandlerFunc) echo.HandlerFunc

func Playbook(action string) MiddlewareFunc {
	return Authorization(policy.ObjectPlaybooks, action)
}

func Catalog(action string) MiddlewareFunc {
	return Authorization(policy.ObjectCatalog, action)
}

func Topology(action string) MiddlewareFunc {
	return Authorization(policy.ObjectTopology, action)
}

func Canary(action string) MiddlewareFunc {
	return Authorization(policy.ObjectCanary, action)
}

func DbMiddleware() MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			if !strings.HasPrefix(path, "/db/") {
				return next(c)
			}

			action := rbac.GetActionFromHttpMethod(c.Request().Method)
			if action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}

			resource := strings.ReplaceAll(path, "/db/", "")
			object := rbac.GetObjectByTable(resource)
			if object == "" {
				return c.String(http.StatusNotFound, "")
			}

			// Scopes, Roles and RoleBindings are validated when they're written, so they're only written through their API
			if route, ok := managedTables[resource]; ok && action != policy.ActionRead {
				return c.String(http.StatusMethodNotAllowed, fmt.Sprintf("%s can't be written through /db; use %s", resource, route))
			}

			ctx := c.Request().Context().(context.Context)

			// Subjects without built-in access are only granted checks through their own object
			if resource == "checks" && !rbac.HasImplicitGrants(ctx.Subject()) {
				object = adapter.ObjectChecks
			}

			if !rbac.CheckContext(ctx, object, action) && !canListFilteredRows(ctx, resource, action) {
				c.Response().Header().Add("X-Rbac-Subject", ctx.Subject())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
			}

			return next(c)
		}
	}
}

// rowFilteredTables are the tables whose rows are filtered by a subject's read grants of their resource type.
var rowFilteredTables = map[string]string{
	"config_items": policy.ResourceConfig,
	"components":   policy.ResourceComponent,
	"checks":       policy.ResourceCheck,
	"canaries":     policy.ResourceCanary,
	"playbooks":    policy.ResourcePlaybook,
}

// ReadGrantsCover reports whether the subject's read grants filter rows of the resource type.
// The auth package sets it, since it builds the row filters.
var ReadGrantsCover func(ctx context.Context, resourceType string) bool

// canListFilteredRows reports whether a subject without built-in access, e.g. a user of an external identity provider,
// can list a table because its read grants cover some rows of it. Postgres then filters the rows.
// Its read grants on whole types pass the object check instead.
func canListFilteredRows(ctx context.Context, table, action string) bool {
	if action != policy.ActionRead || rbac.HasImplicitGrants(ctx.Subject()) || ReadGrantsCover == nil {
		return false
	}

	resourceType, ok := rowFilteredTables[table]
	return ok && ReadGrantsCover(ctx, resourceType)
}

func Authorization(object, action string) MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth if Enforcer is not initialized
			if rbac.Enforcer() == nil {
				return next(c)
			}

			// If action is unset, extract from HTTP Method
			if action == "" {
				action = rbac.GetActionFromHttpMethod(c.Request().Method)
			}

			ctx := c.Request().Context().(context.Context)
			u := ctx.User()

			if u == nil {
				return c.String(http.StatusUnauthorized, "Not logged in")
			}
			if object == "" || action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}

			if !rbac.CheckContext(ctx, object, action) {
				c.Response().Header().Add("X-Rbac-Subject", ctx.Subject())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
			}

			return next(c)
		}
	}
}
