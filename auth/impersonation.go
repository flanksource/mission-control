package auth

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

const (
	HeaderFlanksourceScope = "X-Flanksource-Scope"
	impersonatedRLSCtxKey  = "impersonated-rls-scopes"
)

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

// applyImpersonation narrows the effective RLS payload to the Scopes the X-Flanksource-Scope header names:
// each of them is added to every grant, so it can only narrow. For a subject whose listings aren't filtered,
// the Scopes are the only grant of every type. Rows of views are narrowed to the Scopes' view grants.
// A header naming no Scope lists nothing.
func applyImpersonation(real *rls.Payload, scopeIDs []string) *rls.Payload {
	if scopeIDs == nil {
		return real
	}

	result := &rls.Payload{}
	if len(scopeIDs) == 0 {
		return result
	}

	if real.Disable {
		for _, kind := range rls.GrantTypes {
			grants := rls.AllRows()
			grants.Impersonate(scopeIDs...)
			result.SetGrants(kind, grants)
		}
		result.View = []rls.Scope{{ID: "*"}}
		result.Scopes = slices.Clone(scopeIDs)
		return result
	}

	for _, kind := range rls.GrantTypes {
		if grants := real.GrantsFor(kind); grants != nil {
			narrowed := &rls.Grants{All: grants.All, Any: slices.Clone(grants.Any)}
			narrowed.Impersonate(scopeIDs...)
			result.SetGrants(kind, narrowed)
		}
	}
	result.View = real.View
	result.Scopes = lo.Intersect(real.Scopes, scopeIDs)
	return result
}

// ScopeImpersonation is an echo middleware that reads the X-Flanksource-Scope header, a JSON array of Scope ids,
// and stores them in the request context. GetRLSPayload then narrows the subject's grants to them.
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

		ctx = ctx.WithValue(impersonatedRLSCtxKey, ids)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

// getImpersonatedScopes returns the Scope ids set by the ScopeImpersonation middleware, or nil.
func getImpersonatedScopes(ctx context.Context) []string {
	if v, ok := ctx.Value(impersonatedRLSCtxKey).([]string); ok {
		return v
	}
	return nil
}
