package rbac

import (
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
)

// AuthorizeConfigCreate returns an EFORBIDDEN error unless ctx.Subject(), e.g. a playbook during its run, may create
// configs (create on the catalog, see specs/authorization/roles.md, Section 4.3). It isn't checked again on an agent,
// for an action upstream dispatched to it.
func AuthorizeConfigCreate(ctx context.Context) error {
	if dispatchedFromUpstream(ctx) || rbac.Check(ctx, ctx.Subject(), policy.ObjectCatalog, policy.ActionCreate) {
		return nil
	}

	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		With("permission", policy.ActionCreate, "object", policy.ObjectCatalog).
		Errorf("access denied to %s: create on the catalog required", ctx.Subject())
}
