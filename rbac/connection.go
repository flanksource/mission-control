package rbac

import (
	"encoding/json"
	"strings"

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
	return HasPermission(ctx, subject, &models.ABACAttribute{Connection: conn}, adapter.ActionConnectionUse)
}

// CanReadConnection reports whether subject may see the connection's name, type and settings, without its credentials.
func CanReadConnection(ctx context.Context, subject string, conn models.Connection) bool {
	return HasPermission(ctx, subject, &models.ABACAttribute{Connection: conn}, policy.ActionRead)
}

// AuthorizeConnectionUse returns an EFORBIDDEN error unless the person making the request may act with the
// connection's credentials.
//
// Transitionally, update on all connections is accepted as well: built-in roles don't grant connection:use yet, and
// editor, which can update connections, could test them and fill in their credentials before connection:use existed.
// Either way, a request limited to Scopes the connection isn't in is refused.
func AuthorizeConnectionUse(ctx context.Context, conn models.Connection) error {
	if CanUseConnection(ctx, ctx.Subject(), conn) ||
		(CheckContext(ctx, policy.ObjectConnection, policy.ActionUpdate) && withinScopeLimits(ctx, conn)) {
		return nil
	}

	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		With("permission", adapter.ActionConnectionUse, "connection", conn.ID).
		Errorf("access denied to %s: %s required on connection %s/%s", ctx.Subject(), adapter.ActionConnectionUse, conn.Namespace, conn.Name)
}

// AuthorizeConnection returns an EFORBIDDEN error unless the person making the request may perform the action on
// connections, and the connection is within the request's Scope limits.
func AuthorizeConnection(ctx context.Context, conn models.Connection, action string) error {
	if CheckContext(ctx, policy.ObjectConnection, action) && withinScopeLimits(ctx, conn) {
		return nil
	}

	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		With("permission", action, "connection", conn.ID).
		Errorf("access denied to %s: %s required on connection %s/%s", ctx.Subject(), action, conn.Namespace, conn.Name)
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

// GetConnectionForUse finds the connection a reference names, by id or connection://<namespace>/<name>, and returns
// it with its credentials filled in, if the subject doing the work may use it (connection:use, checked as
// ctx.Subject()). It's for work done as a resource, e.g. a playbook's actions or a notification's send, so there's no
// transitional fallback for people.
func GetConnectionForUse(ctx context.Context, ref string) (*models.Connection, error) {
	conn, err := context.FindConnectionByURL(ctx, ref)
	if err != nil {
		return nil, err
	} else if conn == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("connection (%s) not found", ref)
	}

	if err := authorizeSubjectConnectionUse(ctx, *conn); err != nil {
		return nil, err
	}

	return context.HydrateConnection(ctx, conn)
}

// AuthorizeConnectionRefUse checks connection:use as ctx.Subject() on the connection a reference names, before
// something else fills in its credentials. A reference that isn't a connection id or connection://<namespace>/<name>
// names no stored connection, e.g. a URL, and passes. So does one that names no existing connection: there are no
// credentials to fill in, and the hydration fails on its own.
func AuthorizeConnectionRefUse(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if _, err := uuid.Parse(ref); err != nil && !strings.HasPrefix(ref, "connection://") {
		return nil
	}

	conn, err := context.FindConnectionByURL(ctx, ref)
	if err != nil {
		if dutyAPI.ErrorCode(err) == dutyAPI.ENOTFOUND {
			return nil
		}
		return err
	} else if conn == nil {
		return nil
	}

	return authorizeSubjectConnectionUse(ctx, *conn)
}

// AuthorizeConnectionRefsUse checks connection:use as ctx.Subject() on every connection a spec names, i.e. every
// "connection" field holding a connection id or connection://<namespace>/<name>, at any depth of its JSON form.
func AuthorizeConnectionRefsUse(ctx context.Context, spec any) error {
	b, err := json.Marshal(spec)
	if err != nil {
		return ctx.Oops().Wrap(err)
	}

	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return ctx.Oops().Wrap(err)
	}

	for _, ref := range lo.Uniq(connectionRefs(doc)) {
		if err := AuthorizeConnectionRefUse(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}

func connectionRefs(doc any) []string {
	var refs []string
	switch v := doc.(type) {
	case map[string]any:
		for key, value := range v {
			if s, ok := value.(string); ok && key == "connection" && s != "" {
				refs = append(refs, s)
			} else {
				refs = append(refs, connectionRefs(value)...)
			}
		}
	case []any:
		for _, item := range v {
			refs = append(refs, connectionRefs(item)...)
		}
	}
	return refs
}

func authorizeSubjectConnectionUse(ctx context.Context, conn models.Connection) error {
	if CanUseConnection(ctx, ctx.Subject(), conn) {
		return nil
	}

	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		With("permission", adapter.ActionConnectionUse, "connection", conn.ID).
		Errorf("access denied to %s: %s required on connection %s/%s", ctx.Subject(), adapter.ActionConnectionUse, conn.Namespace, conn.Name)
}
