package rbac

import (
	"github.com/flanksource/duty/rbac/policy"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonManage authorizes a person operation: inviting, creating, disabling, re-enabling, changing or deleting a person
// (specs/authorization/roles.md, Section 2.3).
func PersonManage() MiddlewareFunc {
	return Authorization(policy.ObjectPeople, adapter.ActionPersonManage)
}
