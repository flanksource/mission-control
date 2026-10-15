package rbac

import (
	"github.com/flanksource/duty/rbac/policy"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonManage authorizes an account operation: inviting, disabling, re-enabling, changing or deleting a person
// (specs/authorization/roles.md, Section 2.3).
func PersonManage() MiddlewareFunc {
	return Authorization(policy.ObjectPeople, adapter.ActionPersonManage)
}

// accountTables are the tables /db writes to as account operations, so writing them takes person:manage.
// Reading them is unchanged.
var accountTables = map[string]struct{}{
	"people":     {},
	"identities": {},
}
