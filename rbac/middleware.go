package rbac

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
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
			table, served := servedTable(resource)
			object := rbac.GetObjectByTable(resource)
			if !served || object == "" {
				return c.String(http.StatusNotFound, "")
			}
			if table.kind == dbAction {
				object = table.of[0]
			}

			// The PostgREST roles can't read connections' credentials (flanksource/duty#2114)
			if resource == "connections" {
				return c.String(http.StatusMethodNotAllowed, "connections can't be read or written through /db; read connection_details or connections_list, and write through /connection")
			}

			// Scopes, Roles and RoleBindings are validated when they're written, so they're only written through their API
			if route, ok := managedTables[resource]; ok && action != policy.ActionRead {
				return c.String(http.StatusMethodNotAllowed, fmt.Sprintf("%s can't be written through /db; use %s", resource, route))
			}

			ctx := c.Request().Context().(context.Context)

			// Checks are granted on their own object, not on the canaries object duty's /db map gives the table
			if resource == "checks" {
				object = adapter.ObjectChecks
			}

			if readAction, ok := readOnlyObjects[object]; ok {
				if action != policy.ActionRead {
					return c.String(http.StatusMethodNotAllowed, fmt.Sprintf("%s is read-only", resource))
				}
				action = readAction
			}

			// Only the filtered feature flags are readable without property:manage.
			if resource == "feature_flags" {
				if action != policy.ActionRead {
					return c.String(http.StatusMethodNotAllowed, "feature_flags is read-only; write through /property")
				}
				return next(c)
			}

			if _, ok := accountTables[resource]; ok && action != policy.ActionRead {
				object, action = policy.ObjectPeople, adapter.ActionPersonManage
			}

			allowed := false
			if typeObject, ok := typeTables[resource]; ok {
				// A team's members are part of the team, so changing them takes update on teams
				if resource == "team_members" && action != policy.ActionRead {
					action = policy.ActionUpdate
				}
				object = typeObject
				allowed = CheckContext(ctx, object, action)
			} else {
				allowed = CheckContext(ctx, object, action) || canListFilteredRows(ctx, resource, action) || readsConnections(ctx, resource, action) ||
					scraperGrants(ctx, resource, object, action) || readsWholeTypes(ctx, table, action)
			}

			if !allowed {
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

			if resource == "permissions" {
				if err := refusePermissionDeny(c); err != nil {
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

// readsWholeTypes reports whether the request reads records or a lookup view of types the subject may read entirely.
// They're readable with read on their types (specs/authorization/roles.md, Section 3), but duty's /db map files many
// under legacy objects, e.g. database.public, that no Role grants. A table of several types takes read on each of them,
// so it's never wider than a grant on one type.
func readsWholeTypes(ctx context.Context, table dbTable, action string) bool {
	if action != policy.ActionRead || (table.kind != dbRecord && table.kind != dbLookup) {
		return false
	}
	return lo.EveryBy(table.of, func(resourceType string) bool {
		object := adapter.ObjectForType(resourceType)
		return object != "" && CheckContext(ctx, object, policy.ActionRead)
	})
}

// readOnlyObjects are the objects only read through /db, with the action each read is checked with.
var readOnlyObjects = map[string]string{
	policy.ObjectJobs:   policy.ActionJobRead,
	policy.ObjectEvents: policy.ActionEventRead,
}

// typeTables are the tables of a type whose object differs from the one duty's /db map gives the table,
// with the type's object (rbac/adapter's objectForType), which they're checked on instead.
var typeTables = map[string]string{
	"notification_silences": adapter.ObjectNotificationSilences,
	"notifications_summary": policy.ObjectNotification,
	"agents":                policy.ObjectAgent,
	"agents_summary":        policy.ObjectAgent,
	"teams":                 adapter.ObjectTeams,
	"teams_with_status":     adapter.ObjectTeams,
	"team_members":          adapter.ObjectTeams,
	"connections_list":      policy.ObjectConnection,
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
	owner := rbac.Check(ctx, ctx.Subject(), adapter.ObjectScrapers, action) || rbac.Check(ctx, ctx.Subject(), object, action)
	return owner && TokenAllowsObject(ctx, ctx.Subject(), adapter.ObjectScrapers, action)
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

	// Lookup views of configs, which duty makes security_invoker so that config_items' policy filters them.
	"catalog_changes":         policy.ResourceConfig,
	"config_class_summary":    policy.ResourceConfig,
	"config_classes":          policy.ResourceConfig,
	"config_detail":           policy.ResourceConfig,
	"config_labels":           policy.ResourceConfig,
	"config_names":            policy.ResourceConfig,
	"config_statuses":         policy.ResourceConfig,
	"config_summary":          policy.ResourceConfig,
	"config_tags":             policy.ResourceConfig,
	"config_tags_labels_keys": policy.ResourceConfig,
	"config_types":            policy.ResourceConfig,

	// Lookup views of components, checks and playbooks, which duty makes security_invoker so that their base table's
	// policy filters them. The Health page's views of checks follow canaries, which checks are read through
	// (collection-access.md, Section 5): checks' policy admits a check with its canary, and check_summary joins it.
	"component_labels_keys": policy.ResourceComponent,
	"checks_labels_keys":    policy.ResourceCanary,
	"check_summary":         policy.ResourceCanary,
	"playbook_names":        policy.ResourcePlaybook,

	// A playbook run is read with its playbook, and the config or check it ran on, which its policy checks.
	"playbook_runs": policy.ResourcePlaybook,

	"config_scrapers":             policy.ResourceScraper,
	"config_scrapers_with_status": policy.ResourceScraper,

	"views":         policy.ResourceView,
	"views_summary": policy.ResourceView,
}

// ReadGrantsCover reports whether the subject's read grants filter rows of the resource type.
// The auth package sets it, since it builds the row filters.
var ReadGrantsCover func(ctx context.Context, resourceType string) bool

// canListFilteredRows reports whether the subject can list a table because its read grants cover some rows of it.
// Postgres then filters the rows. Its read grants on whole types pass the object check instead.
// A view's panels and generated table are its records, which Postgres lists with the view.
func canListFilteredRows(ctx context.Context, table, action string) bool {
	if action != policy.ActionRead || ReadGrantsCover == nil || ctx.User() == nil {
		return false
	}

	resourceType, ok := rowFilteredTables[table]
	if t, served := servedTable(table); !ok && served && t.kind == dbRecord && slices.Equal(t.of, []string{policy.ResourceView}) {
		resourceType, ok = policy.ResourceView, true
	}
	return ok && ReadGrantsCover(ctx, resourceType)
}

// ViewRead authorizes reading views through a read of the whole type, or through read grants that cover some views.
// Its handlers must check each view they read.
func ViewRead() MiddlewareFunc {
	return authorize(policy.ObjectViews, policy.ActionRead, func(ctx context.Context) bool {
		return canListFilteredRows(ctx, "views", policy.ActionRead)
	})
}

// PlaybookRead authorizes reading playbooks and their runs through a read of the whole type,
// or through read grants that cover some playbooks.
// Its handlers must query under row-level security, so that Postgres filters the rows.
func PlaybookRead() MiddlewareFunc {
	return authorize(policy.ObjectPlaybooks, policy.ActionRead, func(ctx context.Context) bool {
		return canListFilteredRows(ctx, "playbooks", policy.ActionRead)
	})
}

// CatalogRead authorizes reading the catalog through a read of the whole type,
// or through read grants that cover some configs.
// Its handlers must query under row-level security, so that Postgres filters the rows.
func CatalogRead() MiddlewareFunc {
	return authorize(policy.ObjectCatalog, policy.ActionRead, func(ctx context.Context) bool {
		return canListFilteredRows(ctx, "config_items", policy.ActionRead)
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
