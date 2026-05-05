package auth

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/vars"
)

func getRLSCacheKey(userID string) string {
	return fmt.Sprintf("rls-payload-%s", userID)
}

func InvalidateRLSCacheForUser(userID string) {
	cacheKey := getRLSCacheKey(userID)
	tokenCache.Delete(cacheKey)
}

func GetRLSPayload(ctx context.Context) (*rls.Payload, error) {
	if !ctx.Properties().On(false, vars.FlagRLSEnable) {
		return &rls.Payload{Disable: true}, nil
	}

	impersonated := getImpersonatedPayload(ctx)

	cacheKey := getRLSCacheKey(ctx.User().ID.String())
	if impersonated != nil {
		cacheKey = fmt.Sprintf("%s:%s", cacheKey, impersonated.Fingerprint())
	}

	if cached, ok := tokenCache.Get(cacheKey); ok {
		return cached.(*rls.Payload), nil
	}

	if roles, err := dutyRBAC.RolesForUser(ctx.User().ID.String()); err != nil {
		return nil, err
	} else if !lo.Contains(roles, policy.RoleGuest) {
		payload := &rls.Payload{Disable: true}
		if impersonated != nil {
			result, err := applyImpersonation(payload, impersonated)
			if err != nil {
				return nil, err
			}
			tokenCache.SetDefault(cacheKey, result)
			return result, nil
		}
		tokenCache.SetDefault(cacheKey, payload)
		return payload, nil
	}

	// Build RLS payload from permissions and scopes
	payload, err := buildRLSPayloadFromScopes(ctx)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	if impersonated != nil {
		result, err := applyImpersonation(payload, impersonated)
		if err != nil {
			return nil, err
		}
		tokenCache.SetDefault(cacheKey, result)
		return result, nil
	}

	tokenCache.SetDefault(cacheKey, payload)
	return payload, nil
}

// WithRLS wraps a function with RLS enforcement in a transaction.
// This ensures that Row Level Security is applied to all database queries
// within the function for guest users.
func WithRLS(ctx context.Context, fn func(context.Context) error) error {
	rlsPayload, err := GetRLSPayload(ctx)
	if err != nil {
		return err
	}

	if ctx.Properties().On(false, "rls.debug") {
		ctx.Logger.WithValues("user", lo.FromPtr(ctx.User()).ID).Infof("RLS payload: %s", logger.Pretty(rlsPayload))
	}

	if rlsPayload.Disable {
		return fn(ctx)
	}

	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		if err := rlsPayload.SetPostgresSessionRLS(txCtx.DB()); err != nil {
			return err
		}

		txCtx = txCtx.WithRLSPayload(rlsPayload)
		return fn(txCtx)
	})
}

func buildRLSPayloadFromScopes(ctx context.Context) (*rls.Payload, error) {
	// Get all roles/groups for the user
	roles, err := dutyRBAC.RolesForUser(ctx.User().ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get roles for user: %w", err)
	}

	// Build list of subjects (user ID + roles)
	subjects := append([]string{ctx.User().ID.String()}, roles...)

	var permissions []models.Permission
	err = ctx.DB().
		Where("subject IN ?", subjects).
		Where("action = ?", policy.ActionRead).
		Where("deleted_at IS NULL").
		Where("(object_selector IS NOT NULL) OR playbook_id IS NOT NULL OR canary_id IS NOT NULL OR component_id IS NOT NULL OR config_id IS NOT NULL OR connection_id IS NOT NULL").
		Find(&permissions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}

	payload := &rls.Payload{}

	for _, perm := range permissions {
		if perm.ConfigID != nil {
			addConfigScope(payload, rls.Scope{ID: perm.ConfigID.String()}, perm.Deny)
		}

		if perm.ComponentID != nil {
			addComponentScope(payload, rls.Scope{ID: perm.ComponentID.String()}, perm.Deny)
		}

		if perm.PlaybookID != nil {
			addPlaybookScope(payload, rls.Scope{ID: perm.PlaybookID.String()}, perm.Deny)
		}

		if perm.CanaryID != nil {
			addCanaryScope(payload, rls.Scope{ID: perm.CanaryID.String()}, perm.Deny)
		}

		if len(perm.ObjectSelector) == 0 {
			continue
		}

		var selectors v1.PermissionObject
		if err := json.Unmarshal([]byte(perm.ObjectSelector), &selectors); err != nil {
			ctx.Warnf("failed to unmarshal object_selector for permission %s: %v", perm.ID, err)
			continue
		}

		// Process scope references (indirect permissions)
		if len(selectors.Scopes) > 0 {
			if err := processScopeRefs(ctx, selectors.Scopes, payload, perm.Deny); err != nil {
				return nil, err
			}
		}

		// Process direct resource selectors (configs, components, playbooks, etc.)
		// Only use tags, name, and agent_id as per requirements
		if len(selectors.Configs) > 0 {
			for _, selector := range selectors.Configs {
				addConfigScope(payload, convertResourceSelectorToRLSScope(selector), perm.Deny)
			}
		}

		if len(selectors.Components) > 0 {
			for _, selector := range selectors.Components {
				addComponentScope(payload, convertResourceSelectorToRLSScope(selector), perm.Deny)
			}
		}

		if len(selectors.Playbooks) > 0 {
			for _, selector := range selectors.Playbooks {
				addPlaybookScope(payload, convertResourceSelectorToRLSScope(selector), perm.Deny)
			}
		}

		if len(selectors.Views) > 0 {
			for _, viewRef := range selectors.Views {
				addViewScope(payload, convertViewScopeRefToRLSScope(viewRef), perm.Deny)
			}
		}

		// TODO: No RLS support for connections yet!
		// if len(selectors.Connections) > 0 {
		// 	for _, selector := range selectors.Connections {
		// 		payload.Connections = append(payload.Connections, convertResourceSelectorToRLSScope(selector))
		// 	}
		// }
	}

	return payload, nil
}

func addConfigScope(payload *rls.Payload, scope rls.Scope, deny bool) {
	scope.Deny = deny
	payload.Config = append(payload.Config, scope)
}

func addComponentScope(payload *rls.Payload, scope rls.Scope, deny bool) {
	scope.Deny = deny
	payload.Component = append(payload.Component, scope)
}

func addPlaybookScope(payload *rls.Payload, scope rls.Scope, deny bool) {
	scope.Deny = deny
	payload.Playbook = append(payload.Playbook, scope)
}

func addCanaryScope(payload *rls.Payload, scope rls.Scope, deny bool) {
	scope.Deny = deny
	payload.Canary = append(payload.Canary, scope)
}

func addViewScope(payload *rls.Payload, scope rls.Scope, deny bool) {
	scope.Deny = deny
	payload.View = append(payload.View, scope)
}

// processScopeRefs fetches scopes from database and adds their targets to the payload
func processScopeRefs(ctx context.Context, scopeRefs []dutyRBAC.NamespacedNameIDSelector, payload *rls.Payload, deny bool) error {
	for _, ref := range scopeRefs {
		var scope models.Scope
		err := ctx.DB().
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", ref.Name, ref.Namespace).
			First(&scope).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				ctx.Warnf("scope %s/%s not found", ref.Namespace, ref.Name)
				continue
			}
			return fmt.Errorf("failed to query scope %s/%s: %w", ref.Namespace, ref.Name, err)
		}

		// Add scope UUID for view row-level grants
		if !deny {
			payload.Scopes = append(payload.Scopes, scope.ID.String())
		}

		var targets []v1.ScopeTarget
		if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
			ctx.Warnf("failed to unmarshal targets for scope %s: %v", scope.ID, err)
			continue
		}

		for _, target := range targets {
			if target.Config != nil {
				addConfigScope(payload, convertToRLSScope(target.Config), deny)
			}
			if target.Component != nil {
				addComponentScope(payload, convertToRLSScope(target.Component), deny)
			}
			if target.Playbook != nil {
				addPlaybookScope(payload, convertToRLSScope(target.Playbook), deny)
			}
			if target.Canary != nil {
				addCanaryScope(payload, convertToRLSScope(target.Canary), deny)
			}
			if target.View != nil {
				addViewScope(payload, convertToRLSScope(target.View), deny)
			}
			if target.Global != nil {
				rlsScope := convertToRLSScope(target.Global)
				addConfigScope(payload, rlsScope, deny)
				addComponentScope(payload, rlsScope, deny)
				addPlaybookScope(payload, rlsScope, deny)
				addCanaryScope(payload, rlsScope, deny)
				addViewScope(payload, rlsScope, deny)
			}
		}
	}

	return nil
}

func convertToRLSScope(selector *v1.ScopeResourceSelector) rls.Scope {
	rlsScope := rls.Scope{}

	if selector.Agent != "" {
		rlsScope.Agents = []string{selector.Agent}
	}

	if selector.Name != "" {
		rlsScope.Names = []string{selector.Name}
	}

	if selector.TagSelector != "" {
		rlsScope.Tags = collections.SelectorToMap(selector.TagSelector)
	}

	return rlsScope
}

// convertResourceSelectorToRLSScope converts a types.ResourceSelector to rls.Scope
// Only uses tags, name, and agent_id.
func convertResourceSelectorToRLSScope(selector types.ResourceSelector) rls.Scope {
	rlsScope := rls.Scope{}

	if selector.Agent != "" {
		rlsScope.Agents = []string{selector.Agent}
	}

	if selector.Name != "" {
		rlsScope.Names = []string{selector.Name}
	}

	if selector.TagSelector != "" {
		rlsScope.Tags = collections.SelectorToMap(selector.TagSelector)
	}

	return rlsScope
}

// convertViewScopeRefToRLSScope converts a view ViewRef (namespace/name) to rls.Scope
// Views only support id and name in match_scope (namespace is not supported)
func convertViewScopeRefToRLSScope(viewRef dutyRBAC.ViewRef) rls.Scope {
	rlsScope := rls.Scope{}

	if viewRef.Name != "" {
		rlsScope.Names = []string{viewRef.Name}
	}

	if viewRef.ID != "" {
		rlsScope.ID = viewRef.ID
	}

	// Note: namespace is not supported by match_scope for views
	// ID would be set if we have a direct ID reference, but ViewRef doesn't have ID field

	return rlsScope
}
