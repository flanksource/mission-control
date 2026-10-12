package auth

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

const HeaderFlanksourceScope = "X-Flanksource-Scope"

// parseImpersonatedScopes parses the X-Flanksource-Scope header: a JSON array of Scope ids.
func parseImpersonatedScopes(header string) ([]string, error) {
	var ids []string
	if err := json.Unmarshal([]byte(header), &ids); err != nil {
		return nil, fmt.Errorf("must be a JSON array of scope ids: %w", err)
	}

	for i, id := range ids {
		parsed, err := uuid.Parse(strings.TrimSpace(id))
		if err != nil {
			return nil, fmt.Errorf("scope id %q isn't a uuid", id)
		}
		ids[i] = parsed.String()
	}

	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// applyScopeLimits limits the effective RLS payload to the request's Scope limits (rls.Grants.Limit): a row must be
// in at least one Scope of each limit, on top of what the subject's grants list. For a subject whose listings
// aren't filtered, the limits are the only grants of every type. A limit naming no Scope lists nothing.
func applyScopeLimits(real *rls.Payload, limits [][]string) *rls.Payload {
	if limits == nil {
		return real
	}

	result := &rls.Payload{}
	scopes := real.Scopes
	if real.Disable {
		scopes = limits[0]
	}
	for _, limit := range limits {
		if len(limit) == 0 {
			return result
		}
		scopes = lo.Intersect(scopes, limit)
	}
	result.Scopes = scopes

	for _, kind := range rls.GrantTypes {
		grants := real.GrantsFor(kind)
		if real.Disable {
			grants = rls.AllRows()
		} else if grants == nil {
			continue
		}

		limited := &rls.Grants{All: grants.All, Any: slices.Clone(grants.Any)}
		for _, limit := range limits {
			limited.Limit(limit...)
		}
		result.SetGrants(kind, limited)
	}

	result.View = real.View
	if real.Disable {
		result.View = []rls.Scope{{ID: "*"}}
	}
	return result
}

// ScopeImpersonation is an echo middleware that reads the X-Flanksource-Scope header, a JSON array of Scope ids,
// and limits the request to them (dutyRBAC.LimitToScopes): GetRLSPayload limits the subject's listings, and duty's
// rbac.HasPermission its checks on resources, by the same rule.
func ScopeImpersonation(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		ctx := c.Request().Context().(context.Context)

		if ctx.Properties().Off("auth.impersonation", false) {
			return next(c)
		}

		header := c.Request().Header.Get(HeaderFlanksourceScope)
		if header == "" {
			return next(c)
		}

		ids, err := parseImpersonatedScopes(header)
		if err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid %s header: %v", HeaderFlanksourceScope, err))
		}

		ctx = dutyRBAC.LimitToScopes(ctx, ids)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
