package rbac

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonManage authorizes an account operation: inviting, disabling, re-enabling, changing or deleting a person
// (specs/authorization/roles.md, Section 2.3). legacyObject and legacyAction are what the route checked before
// person:manage existed; see CanManagePeople.
func PersonManage(legacyObject, legacyAction string) MiddlewareFunc {
	return authorize(policy.ObjectPeople, adapter.ActionPersonManage, func(ctx context.Context) bool {
		return legacyPersonManage(ctx, legacyObject, legacyAction)
	})
}

// CanManagePeople reports whether the subject may perform an account operation: person:manage, or transitionally
// the operation's legacy check (see legacyPersonManage).
func CanManagePeople(ctx context.Context, legacyObject, legacyAction string) bool {
	return CheckContext(ctx, policy.ObjectPeople, adapter.ActionPersonManage) || legacyPersonManage(ctx, legacyObject, legacyAction)
}

// legacyPersonManage is TRANSITIONAL: it accepts the check an account operation made before person:manage existed,
// e.g. auth:update for inviting, so the built-in roles that pass it today (admin) keep passing.
// No Role can grant these: create, update and delete don't take people, and auth, rbac and database.public aren't types.
//
// Remove it, and the legacy object and action passed to PersonManage and CanManagePeople, with the built-in roles.
func legacyPersonManage(ctx context.Context, legacyObject, legacyAction string) bool {
	return legacyObject != "" && legacyAction != "" && CheckContext(ctx, legacyObject, legacyAction)
}

// accountTables are the tables /db writes to as account operations, so writing them takes person:manage.
// Reading them is unchanged.
var accountTables = map[string]struct{}{
	"people":     {},
	"identities": {},
}
