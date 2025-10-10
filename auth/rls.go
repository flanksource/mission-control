package auth

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/vars"
)

func GetRLSPayload(ctx context.Context) (*rls.Payload, error) {
	if !ctx.Properties().On(false, vars.FlagRLSEnable) {
		return &rls.Payload{Disable: true}, nil
	}

	cacheKey := fmt.Sprintf("rls-payload-%s", ctx.User().ID.String())
	if cached, ok := tokenCache.Get(cacheKey); ok {
		return cached.(*rls.Payload), nil
	}

	if roles, err := dutyRBAC.RolesForUser(ctx.User().ID.String()); err != nil {
		return nil, err
	} else if !lo.Contains(roles, policy.RoleGuest) {
		payload := &rls.Payload{Disable: true}
		tokenCache.SetDefault(cacheKey, payload)
		return payload, nil
	}

	// Build RLS payload from permissions and scopes
	payload, err := buildRLSPayloadFromScopes(ctx)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	tokenCache.SetDefault(cacheKey, payload)
	return payload, nil
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
			payload.Config = append(payload.Config, rls.Scope{ID: perm.ConfigID.String()})
		}

		if perm.ComponentID != nil {
			payload.Component = append(payload.Component, rls.Scope{ID: perm.ComponentID.String()})
		}

		if perm.PlaybookID != nil {
			payload.Playbook = append(payload.Playbook, rls.Scope{ID: perm.PlaybookID.String()})
		}

		if perm.CanaryID != nil {
			payload.Canary = append(payload.Canary, rls.Scope{ID: perm.CanaryID.String()})
		}

		if len(perm.ObjectSelector) == 0 {
			continue
		}

		var selectors dutyRBAC.Selectors
		if err := json.Unmarshal([]byte(perm.ObjectSelector), &selectors); err != nil {
			ctx.Warnf("failed to unmarshal object_selector for permission %s: %v", perm.ID, err)
			continue
		}

		// Process scope references (indirect permissions)
		if len(selectors.Scopes) > 0 {
			if err := processScopeRefs(ctx, selectors.Scopes, payload); err != nil {
				return nil, err
			}
		}

		// Process direct resource selectors (configs, components, playbooks, etc.)
		// Only use tags, name, and agent_id as per requirements
		if len(selectors.Configs) > 0 {
			for _, selector := range selectors.Configs {
				payload.Config = append(payload.Config, convertResourceSelectorToRLSScope(selector))
			}
		}

		if len(selectors.Components) > 0 {
			for _, selector := range selectors.Components {
				payload.Component = append(payload.Component, convertResourceSelectorToRLSScope(selector))
			}
		}

		if len(selectors.Playbooks) > 0 {
			for _, selector := range selectors.Playbooks {
				payload.Playbook = append(payload.Playbook, convertResourceSelectorToRLSScope(selector))
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

// processScopeRefs fetches scopes from database and adds their targets to the payload
func processScopeRefs(ctx context.Context, scopeRefs []dutyRBAC.ScopeRef, payload *rls.Payload) error {
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

		var targets []v1.ScopeTarget
		if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
			ctx.Warnf("failed to unmarshal targets for scope %s: %v", scope.ID, err)
			continue
		}

		for _, target := range targets {
			if target.Config != nil {
				rlsScope := convertToRLSScope(target.Config)
				payload.Config = append(payload.Config, rlsScope)
			}
			if target.Component != nil {
				rlsScope := convertToRLSScope(target.Component)
				payload.Component = append(payload.Component, rlsScope)
			}
			if target.Playbook != nil {
				rlsScope := convertToRLSScope(target.Playbook)
				payload.Playbook = append(payload.Playbook, rlsScope)
			}
			if target.Canary != nil {
				rlsScope := convertToRLSScope(target.Canary)
				payload.Canary = append(payload.Canary, rlsScope)
			}
			if target.Global != nil {
				rlsScope := convertToRLSScope(target.Global)
				payload.Config = append(payload.Config, rlsScope)
				payload.Component = append(payload.Component, rlsScope)
				payload.Playbook = append(payload.Playbook, rlsScope)
				payload.Canary = append(payload.Canary, rlsScope)
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
