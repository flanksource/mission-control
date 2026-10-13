package rbac

import (
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// CanUseConnection reports whether subject may act with the connection's credentials (connection:use, see
// specs/authorization/roles.md, Section 2.2). It's checked on the connection, by its stored Scope membership.
func CanUseConnection(ctx context.Context, subject string, conn models.Connection) bool {
	return rbac.HasPermission(ctx, subject, &models.ABACAttribute{Connection: conn}, adapter.ActionConnectionUse)
}

// CanReadConnection reports whether subject may see the connection's name, type and settings, without its credentials.
func CanReadConnection(ctx context.Context, subject string, conn models.Connection) bool {
	return rbac.HasPermission(ctx, subject, &models.ABACAttribute{Connection: conn}, policy.ActionRead)
}

// AuthorizeConnectionUse returns an EFORBIDDEN error unless the person making the request may act with the
// connection's credentials.
//
// Transitionally, update on all connections is accepted as well: built-in roles don't grant connection:use yet, and
// editor, which can update connections, could test them and fill in their credentials before connection:use existed.
// Either way, a request limited to Scopes the connection isn't in is refused.
func AuthorizeConnectionUse(ctx context.Context, conn models.Connection) error {
	if CanUseConnection(ctx, ctx.Subject(), conn) ||
		(rbac.CheckContext(ctx, policy.ObjectConnection, policy.ActionUpdate) && withinScopeLimits(ctx, conn)) {
		return nil
	}

	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		With("permission", adapter.ActionConnectionUse, "connection", conn.ID).
		Errorf("access denied to %s: %s required on connection %s/%s", ctx.Subject(), adapter.ActionConnectionUse, conn.Namespace, conn.Name)
}

// withinScopeLimits reports whether the connection is in a Scope of each of the request's Scope limits, as
// HasPermission requires of the resources it checks. Checks on whole object types don't apply the limits themselves.
func withinScopeLimits(ctx context.Context, conn models.Connection) bool {
	limits := rbac.ScopeLimits(ctx)
	if len(limits) == 0 {
		return true
	}

	ref := membership.Ref{Type: policy.ResourceConnection, ID: conn.ID}
	snapshot, err := membership.Read(ctx, ref)
	if err != nil {
		ctx.Errorf("failed to read scope membership of connection %s: %v", conn.ID, err)
		return false
	}

	scopes := lo.Map(snapshot.Scopes(ref), func(id uuid.UUID, _ int) string { return id.String() })
	for _, limit := range limits {
		if len(lo.Intersect(limit, scopes)) == 0 {
			return false
		}
	}
	return true
}
