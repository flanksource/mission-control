package rbac

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

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

			if readAction, ok := readOnlyObjects[object]; ok {
				if action != policy.ActionRead {
					return c.String(http.StatusMethodNotAllowed, fmt.Sprintf("%s is read-only", resource))
				}
				action = readAction
			}

			// Any signed-in subject may read properties; writing them takes property:manage
			if resource == "properties" {
				if action == policy.ActionRead {
					return next(c)
				}
				object, action = policy.ObjectProperties, policy.ActionPropertyManage
			}

			allowed := false
			if typeObject, ok := typeTables[resource]; ok {
				typeAction := action
				// A team's members are part of the team, so changing them takes update on teams
				if resource == "team_members" && action != policy.ActionRead {
					typeAction = policy.ActionUpdate
				}
				allowed = CheckType(ctx, Grant{typeObject, typeAction}, Grant{object, action})
				object, action = typeObject, typeAction
			} else {
				allowed = CheckContext(ctx, object, action) || canListFilteredRows(ctx, resource, action) || readsConnections(ctx, resource, action) ||
					scraperGrants(ctx, resource, object, action)
			}

			if !allowed || scraperDenied(ctx, resource, object, action) {
				c.Response().Header().Add("X-Rbac-Subject", ctx.Subject())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
			}

			if resource == "teams" && action != policy.ActionRead {
				if err := guardShippedTeams(c, ctx); err != nil {
					return dutyAPI.WriteError(c, err)
				}
			}

			return next(c)
		}
	}
}

// readsConnections reports whether the request reads connection_details, which reading connections allows: it's how
// connections are read, with their credentials masked (specs/authorization/roles.md, Section 2.2).
func readsConnections(ctx context.Context, table, action string) bool {
	return table == "connection_details" && action == policy.ActionRead && CheckContext(ctx, policy.ObjectConnection, policy.ActionRead)
}

// readOnlyObjects are the objects only read through /db, with the action each read is checked with.
var readOnlyObjects = map[string]string{
	policy.ObjectJobs:   policy.ActionJobRead,
	policy.ObjectEvents: policy.ActionEventRead,
}

// typeTables are the tables of a type whose object differs from the one duty's /db map gives the table,
// with the type's object (rbac/adapter's objectForType). duty's object for the table is still granted by
// the built-in roles, so it's checked as the legacy grant (see CheckType).
var typeTables = map[string]string{
	"notification_silences": adapter.ObjectNotificationSilences,
	"notifications_summary": policy.ObjectNotification,
	"agents":                policy.ObjectAgent,
	"agents_summary":        policy.ObjectAgent,
	"teams":                 adapter.ObjectTeams,
	"team_members":          adapter.ObjectTeams,
	"scopes":                adapter.ObjectScopes,
	"scope_targets":         adapter.ObjectScopes,
	"roles":                 adapter.ObjectRoles,
	"role_bindings":         adapter.ObjectRoleBindings,
}

// scraperGrants reports whether the subject's grants on scrapers allow the action on config scrapers. duty files the
// table under database.config_scrapers, which logging backends share, and its view under database.monitor, so
// Role rules on scrapers are granted on an object of their own. The subject may hold either object, but an access
// token's Roles must allow scrapers: they're Roles, so they grant types, not the table's object.
func scraperGrants(ctx context.Context, table, object, action string) bool {
	switch table {
	case "config_scrapers":
	case "config_scrapers_with_status":
		if action != policy.ActionRead {
			return false
		}
	default:
		return false
	}

	user := ctx.User()
	if user == nil {
		return false
	}
	owner := rbac.CheckContext(ctx, adapter.ObjectScrapers, action) || rbac.CheckContext(ctx, object, action)
	return owner && TokenAllowsObject(ctx, user.ID.String(), adapter.ObjectScrapers, action)
}

// scraperDenied reports whether a deny on any object a scraper table is granted through applies to the action: the
// table's own object (database.monitor for the status view), database.config_scrapers and scrapers. A grant on one
// of them can't bypass a deny on another.
func scraperDenied(ctx context.Context, table, object, action string) bool {
	if (table != "config_scrapers" && table != "config_scrapers_with_status") || ctx.User() == nil || rbac.Enforcer() == nil {
		return false
	}
	subject := checkedSubject(ctx)
	for _, o := range lo.Uniq([]string{object, policy.ObjectDatabaseSettings, adapter.ObjectScrapers}) {
		if deniedExplicitly(ctx, subject, Grant{o, action}) {
			return true
		}
	}
	return false
}

// rowFilteredTables are the tables whose rows are filtered by a subject's read grants of their resource type.
var rowFilteredTables = map[string]string{
	"config_items":   policy.ResourceConfig,
	"configs":        policy.ResourceConfig,
	"config_changes": policy.ResourceConfig,
	"components":     policy.ResourceComponent,
	"checks":         policy.ResourceCheck,
	"canaries":       policy.ResourceCanary,
	"playbooks":      policy.ResourcePlaybook,

	"config_scrapers":             policy.ResourceScraper,
	"config_scrapers_with_status": policy.ResourceScraper,
}

// ReadGrantsCover reports whether the subject's read grants filter rows of the resource type.
// The auth package sets it, since it builds the row filters.
var ReadGrantsCover func(ctx context.Context, resourceType string) bool

// canListFilteredRows reports whether a guest, a subject without built-in access or a request made with a token
// limited by Roles can list a table because its read grants cover some rows of it. Postgres then filters the rows.
// Its read grants on whole types pass the object check instead.
func canListFilteredRows(ctx context.Context, table, action string) bool {
	if action != policy.ActionRead || ReadGrantsCover == nil || ctx.User() == nil {
		return false
	}
	if rbac.HasImplicitGrants(ctx.Subject()) && !isGuest(ctx) {
		if _, limited := TokenLimit(ctx, ctx.User().ID.String()); !limited {
			return false
		}
	}

	resourceType, ok := rowFilteredTables[table]
	return ok && ReadGrantsCover(ctx, resourceType)
}

// isGuest reports whether the subject is a person with the guest role.
// When the roles can't be read, it reports false, so that a non-guest isn't let through.
func isGuest(ctx context.Context) bool {
	user := ctx.User()
	if user == nil {
		return false
	}

	roles, err := rbac.RolesForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("failed to get roles of %s: %v", user.ID, err)
		return false
	}
	return lo.Contains(roles, policy.RoleGuest)
}

// PlaybookList authorizes listing playbooks through a read of the whole type,
// or through read grants that cover some playbooks.
// Its handler must query under row-level security, so that Postgres filters the rows.
func PlaybookList() MiddlewareFunc {
	return authorize(policy.ObjectPlaybooks, policy.ActionRead, func(ctx context.Context) bool {
		return canListFilteredRows(ctx, "playbooks", policy.ActionRead)
	})
}

func Authorization(object, action string) MiddlewareFunc {
	return authorize(object, action, nil)
}

// authorize checks the action on the whole object type. A subject that fails the check still passes when orElse allows it.
func authorize(object, action string, orElse func(ctx context.Context) bool) MiddlewareFunc {
	return authorizeWith(object, action, func(ctx context.Context, action string) bool {
		return CheckContext(ctx, object, action) || (orElse != nil && orElse(ctx))
	})
}

// authorizeWith authorizes a request when check allows the action. object and action are reported when it doesn't.
func authorizeWith(object, action string, check func(ctx context.Context, action string) bool) MiddlewareFunc {
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

			if !check(ctx, action) {
				c.Response().Header().Add("X-Rbac-Subject", ctx.Subject())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
			}

			return next(c)
		}
	}
}
