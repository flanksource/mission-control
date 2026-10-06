package rbac

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/samber/lo"
)

// ReadAccess is how much of a resource type a subject may read.
type ReadAccess string

const (
	ReadAll  ReadAccess = "all"
	ReadSome ReadAccess = "some"
	ReadNone ReadAccess = "none"
)

// summaryObjects are the resource types the access summary reports, with the RBAC object a read of the whole type is granted on.
var summaryObjects = map[string]string{
	policy.ResourceConfig:     policy.ObjectCatalog,
	policy.ResourceComponent:  policy.ObjectTopology,
	policy.ResourceCanary:     policy.ObjectCanary,
	policy.ResourcePlaybook:   policy.ObjectPlaybooks,
	policy.ResourceConnection: policy.ObjectConnection,
}

// ReadAccessSummary reports, for each resource type, whether the subject may read all, some or none of it.
//
// A read of the whole type gives all. Otherwise, read grants that filter rows of the type give some,
// even when they currently match no resource.
func ReadAccessSummary(ctx context.Context) map[string]ReadAccess {
	guest := isGuest(ctx)

	summary := make(map[string]ReadAccess, len(summaryObjects))
	for resourceType, object := range summaryObjects {
		switch {
		case readsWholeType(ctx, object, guest):
			summary[resourceType] = ReadAll
		case ReadGrantsCover != nil && ReadGrantsCover(ctx, resourceType):
			summary[resourceType] = ReadSome
		default:
			summary[resourceType] = ReadNone
		}
	}

	return summary
}

// readsWholeType reports whether the subject may read every resource of the object.
// A guest passes whole-type checks as a viewer only for its listings to be filtered by row,
// so only its own grants count.
func readsWholeType(ctx context.Context, object string, guest bool) bool {
	if guest {
		return rbac.Check(ctx, ctx.User().ID.String(), object, policy.ActionRead)
	}
	return rbac.CheckContext(ctx, object, policy.ActionRead)
}

// isGuest reports whether the subject is a person with the guest role.
// When the roles can't be read, it reports true, so that only the subject's own grants count.
func isGuest(ctx context.Context) bool {
	user := ctx.User()
	if user == nil || !rbac.HasImplicitGrants(ctx.Subject()) {
		return false
	}

	roles, err := rbac.RolesForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("failed to get roles of %s: %v", user.ID, err)
		return true
	}
	return lo.Contains(roles, policy.RoleGuest)
}
