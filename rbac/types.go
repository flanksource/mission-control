package rbac

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/samber/lo"
)

// Grant is an action on an RBAC object.
type Grant struct {
	Object string
	Action string
}

// TypeAuthorization checks the action on the whole of a resource type, on the type's own object (roles.md §2).
// legacy is what the route checked before the type had an object of its own; see CheckType.
func TypeAuthorization(grant, legacy Grant) MiddlewareFunc {
	return authorizeWith(grant.Object, grant.Action, func(ctx context.Context, _ string) bool {
		return CheckType(ctx, grant, legacy)
	})
}

// CheckType reports whether the subject may perform the action on the whole of a resource type,
// checked on the type's own object, or on legacy through a built-in role (see legacyGrant).
func CheckType(ctx context.Context, grant, legacy Grant) bool {
	return CheckContext(ctx, grant.Object, grant.Action) || legacyGrant(ctx, grant, legacy)
}

// builtInGrants are the grants of duty's built-in roles (its policies.yaml) on the legacy objects routes checked,
// with each role's inherited grants included. admin is granted everything.
// They're listed here rather than read from the enforcer, because RoleBindings can select guest, everyone and agent:
// what the enforcer grants those roles includes what bindings grant them.
var builtInGrants = map[string][]Grant{
	policy.RoleViewer:    viewerGrants,
	policy.RoleCommander: viewerGrants,
	policy.RoleResponder: viewerGrants,
	policy.RoleEditor: append([]Grant{
		{policy.ObjectNotification, policy.ActionCreate},
		{policy.ObjectNotification, policy.ActionRead},
		{policy.ObjectNotification, policy.ActionUpdate},
		{policy.ObjectNotification, policy.ActionDelete},
	}, viewerGrants...),
	policy.RoleAgent: {{policy.ObjectDatabasePublic, policy.ActionRead}},
}

var viewerGrants = []Grant{
	{policy.ObjectDatabasePublic, policy.ActionRead},
	{policy.ObjectCatalog, policy.ActionRead},
}

// legacyGrant reports whether one of the subject's built-in roles grants the legacy object and action.
//
// TRANSITIONAL: built-in roles grant the objects routes checked before each type had an object of its own,
// e.g. notification for notification silences, or database.public for agents and teams. Until they're removed,
// a subject keeps what its built-in role gave it:
//   - Only built-in roles count: a RoleBinding's grant on a legacy object is a grant on another type,
//     e.g. catalog for configs, and never opens this type's routes.
//   - The subject must still pass the legacy check itself.
//   - An access token's Roles must allow the type's own object: they're Roles, so they grant types, not legacy objects.
//
// Remove this, builtInGrants, and the legacy grants passed to it, with the built-in roles.
func legacyGrant(ctx context.Context, grant, legacy Grant) bool {
	if legacy.Object == "" || legacy.Action == "" {
		return false
	}

	subject := checkedSubject(ctx)
	if subject == "" {
		return false
	}
	if !rbac.CheckContext(ctx, legacy.Object, legacy.Action) {
		return false
	}
	if !TokenAllowsObject(ctx, ctx.User().ID.String(), grant.Object, grant.Action) {
		return false
	}

	roles, err := rbac.RolesForUser(subject)
	if err != nil {
		ctx.Warnf("failed to get roles of %s: %v", subject, err)
		return false
	}

	for _, role := range lo.Uniq(append(roles, subject)) {
		if role == policy.RoleAdmin || lo.Contains(builtInGrants[role], legacy) {
			return true
		}
	}
	return false
}

// checkedSubject is the subject rbac.CheckContext checks.
func checkedSubject(ctx context.Context) string {
	if subject := ctx.Subject(); !rbac.HasImplicitGrants(subject) {
		return subject
	}
	if user := ctx.User(); user != nil {
		return user.ID.String()
	}
	return ""
}
