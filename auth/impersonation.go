// ABOUTME: Scope impersonation allows users to simulate RLS restrictions
// ABOUTME: via the X-Flanksource-Scope header for testing and debugging.
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rls"
	echov4 "github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

const (
	HeaderFlanksourceScope = "X-Flanksource-Scope"
	impersonatedRLSCtxKey  = "impersonated-rls-payload"
)

// intersectScope computes the intersection of two scopes. Returns the
// intersected scope and true if the result is valid, or false if the two
// scopes are incompatible (e.g. conflicting tags or disjoint ID lists).
//
// Empty fields are treated as unrestricted: if one side has Agents=[] and the
// other has Agents=[a], the result is Agents=[a].
func intersectScope(a, b rls.Scope) (rls.Scope, bool) {
	var result rls.Scope

	// IDs: if both set, must match
	switch {
	case a.ID != "" && b.ID != "":
		if a.ID != b.ID {
			return rls.Scope{}, false
		}
		result.ID = a.ID
	case a.ID != "":
		result.ID = a.ID
	case b.ID != "":
		result.ID = b.ID
	}

	// Tags: merge maps; conflicting values make the intersection empty
	if len(a.Tags) > 0 || len(b.Tags) > 0 {
		merged := make(map[string]string)
		for k, v := range a.Tags {
			merged[k] = v
		}
		for k, v := range b.Tags {
			if existing, ok := merged[k]; ok && existing != v {
				return rls.Scope{}, false
			}
			merged[k] = v
		}
		result.Tags = merged
	}

	// Agents: empty = unrestricted; both non-empty = intersect
	switch {
	case len(a.Agents) == 0:
		result.Agents = b.Agents
	case len(b.Agents) == 0:
		result.Agents = a.Agents
	default:
		result.Agents = lo.Intersect(a.Agents, b.Agents)
		if len(result.Agents) == 0 {
			return rls.Scope{}, false
		}
	}

	// Names: empty = unrestricted; both non-empty = intersect
	switch {
	case len(a.Names) == 0:
		result.Names = b.Names
	case len(b.Names) == 0:
		result.Names = a.Names
	default:
		result.Names = lo.Intersect(a.Names, b.Names)
		if len(result.Names) == 0 {
			return rls.Scope{}, false
		}
	}

	return result, true
}

// intersectScopeList computes the cartesian-product intersection of two scope
// lists. Each scope in the real list is intersected with each scope in the
// impersonated list; only compatible pairs survive.
func intersectScopeList(real, impersonated []rls.Scope) []rls.Scope {
	if len(real) == 0 || len(impersonated) == 0 {
		return nil
	}

	var result []rls.Scope
	for _, r := range real {
		for _, i := range impersonated {
			if s, ok := intersectScope(r, i); ok {
				result = append(result, s)
			}
		}
	}
	return result
}

// intersectPayload computes the intersection of two RLS payloads across all
// resource types.
func intersectPayload(real, impersonated *rls.Payload) *rls.Payload {
	result := &rls.Payload{
		Config:    intersectScopeList(real.Config, impersonated.Config),
		Component: intersectScopeList(real.Component, impersonated.Component),
		Playbook:  intersectScopeList(real.Playbook, impersonated.Playbook),
		Canary:    intersectScopeList(real.Canary, impersonated.Canary),
		View:      intersectScopeList(real.View, impersonated.View),
		Scopes:    lo.Intersect(real.Scopes, impersonated.Scopes),
	}
	return result
}

// applyImpersonation decides the effective RLS payload when impersonation is
// requested. For users with full access (Disable: true) the impersonated
// payload is used directly. For users with existing restrictions the result
// is the intersection of both payloads, ensuring no privilege escalation.
func applyImpersonation(real *rls.Payload, impersonated *rls.Payload) (*rls.Payload, error) {
	if impersonated == nil {
		return real, nil
	}

	if real.Disable {
		return impersonated, nil
	}

	return intersectPayload(real, impersonated), nil
}

// ScopeImpersonation is an echo middleware that reads the X-Flanksource-Scope
// header and stores the parsed RLS payload in the request context. The payload
// is later picked up by GetRLSPayload to override the user's real permissions.
func ScopeImpersonation(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		ctx := c.Request().Context().(context.Context)

		if !ctx.Properties().On(false, "auth.impersonation") {
			return next(c)
		}

		header := c.Request().Header.Get(HeaderFlanksourceScope)
		if header == "" {
			return next(c)
		}

		var payload rls.Payload
		if err := json.Unmarshal([]byte(header), &payload); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("invalid %s header: %v", HeaderFlanksourceScope, err),
			})
		}

		ctx = ctx.WithValue(impersonatedRLSCtxKey, &payload)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

// getImpersonatedPayload retrieves the impersonated RLS payload from context,
// if one was set by the ScopeImpersonation middleware.
func getImpersonatedPayload(ctx context.Context) *rls.Payload {
	if v := ctx.Value(impersonatedRLSCtxKey); v != nil {
		return v.(*rls.Payload)
	}
	return nil
}
