package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
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

	cacheKey := getRLSCacheKey(ctx.User().ID.String())
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

	scopeIDs := map[uuid.UUID]struct{}{}
	wildcards := map[rls.WildcardResourceScope]struct{}{}

	for _, perm := range permissions {
		if !collections.MatchItems(policy.ActionRead, strings.Split(perm.Action, ",")...) {
			continue
		}

		permScopeID := perm.ID

		if perm.ConfigID != nil || perm.ComponentID != nil || perm.PlaybookID != nil || perm.CanaryID != nil {
			scopeIDs[permScopeID] = struct{}{}
		}

		switch perm.Object {
		case policy.ObjectCatalog:
			wildcards[rls.WildcardResourceScopeConfig] = struct{}{}
		case policy.ObjectTopology:
			wildcards[rls.WildcardResourceScopeComponent] = struct{}{}
		case policy.ObjectCanary:
			wildcards[rls.WildcardResourceScopeCanary] = struct{}{}
		case policy.ObjectPlaybooks:
			wildcards[rls.WildcardResourceScopePlaybook] = struct{}{}
		case policy.ObjectViews:
			wildcards[rls.WildcardResourceScopeView] = struct{}{}
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
			if err := processScopeRefs(ctx, selectors.Scopes, scopeIDs, wildcards); err != nil {
				return nil, err
			}
		}

		// Process direct resource selectors (configs, components, playbooks, etc.)
		if len(selectors.Configs) > 0 {
			if hasWildcardSelector(selectors.Configs) {
				wildcards[rls.WildcardResourceScopeConfig] = struct{}{}
			} else {
				scopeIDs[permScopeID] = struct{}{}
			}
		}

		if len(selectors.Components) > 0 {
			if hasWildcardSelector(selectors.Components) {
				wildcards[rls.WildcardResourceScopeComponent] = struct{}{}
			} else {
				scopeIDs[permScopeID] = struct{}{}
			}
		}

		if len(selectors.Playbooks) > 0 {
			if hasWildcardSelector(selectors.Playbooks) {
				wildcards[rls.WildcardResourceScopePlaybook] = struct{}{}
			} else {
				scopeIDs[permScopeID] = struct{}{}
			}
		}

		if len(selectors.Views) > 0 {
			if hasWildcardViewRef(selectors.Views) {
				wildcards[rls.WildcardResourceScopeView] = struct{}{}
			} else {
				scopeIDs[permScopeID] = struct{}{}
			}
		}

		// TODO: No RLS support for connections yet!
		// if len(selectors.Connections) > 0 {
		// 	for _, selector := range selectors.Connections {
		// 		payload.Connections = append(payload.Connections, convertResourceSelectorToRLSScope(selector))
		// 	}
		// }
	}

	payload := &rls.Payload{
		Scopes:         setToSortedUUIDSlice(scopeIDs),
		WildcardScopes: setToSortedWildcardSlice(wildcards),
	}

	return payload, nil
}

// processScopeRefs fetches scopes from database and adds their IDs and wildcard types
func processScopeRefs(ctx context.Context, scopeRefs []dutyRBAC.NamespacedNameIDSelector, scopeIDs map[uuid.UUID]struct{}, wildcards map[rls.WildcardResourceScope]struct{}) error {
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

		// Always include scope UUID for view row-level grants
		scopeIDs[scope.ID] = struct{}{}

		var targets []v1.ScopeTarget
		if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
			ctx.Warnf("failed to unmarshal targets for scope %s: %v", scope.ID, err)
			continue
		}

		for _, target := range targets {
			switch {
			case target.Config != nil:
				if isWildcardScopeSelector(target.Config) {
					wildcards[rls.WildcardResourceScopeConfig] = struct{}{}
				}
			case target.Component != nil:
				if isWildcardScopeSelector(target.Component) {
					wildcards[rls.WildcardResourceScopeComponent] = struct{}{}
				}
			case target.Playbook != nil:
				if isWildcardScopeSelector(target.Playbook) {
					wildcards[rls.WildcardResourceScopePlaybook] = struct{}{}
				}
			case target.Canary != nil:
				if isWildcardScopeSelector(target.Canary) {
					wildcards[rls.WildcardResourceScopeCanary] = struct{}{}
				}
			case target.View != nil:
				if isWildcardScopeSelector(target.View) {
					wildcards[rls.WildcardResourceScopeView] = struct{}{}
				}
			case target.Global != nil:
				if isWildcardScopeSelector(target.Global) {
					wildcards[rls.WildcardResourceScopeConfig] = struct{}{}
					wildcards[rls.WildcardResourceScopeComponent] = struct{}{}
					wildcards[rls.WildcardResourceScopePlaybook] = struct{}{}
					wildcards[rls.WildcardResourceScopeCanary] = struct{}{}
					wildcards[rls.WildcardResourceScopeView] = struct{}{}
				}
			}
		}
	}

	return nil
}

func isWildcardScopeSelector(selector *v1.ScopeResourceSelector) bool {
	if selector == nil {
		return false
	}

	return selector.Name == "*" &&
		selector.Namespace == "" &&
		selector.Agent == "" &&
		selector.TagSelector == ""
}

func hasWildcardSelector(selectors []types.ResourceSelector) bool {
	for _, selector := range selectors {
		if selector.Wildcard() {
			return true
		}
	}
	return false
}

func hasWildcardViewRef(selectors []dutyRBAC.ViewRef) bool {
	for _, selector := range selectors {
		if selector.Name == "*" && selector.Namespace == "" && selector.ID == "" {
			return true
		}
	}
	return false
}

func setToSortedUUIDSlice(set map[uuid.UUID]struct{}) []uuid.UUID {
	if len(set) == 0 {
		return nil
	}

	out := make([]uuid.UUID, 0, len(set))
	for val := range set {
		out = append(out, val)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].String() < out[j].String()
	})

	return out
}

func setToSortedWildcardSlice(set map[rls.WildcardResourceScope]struct{}) []rls.WildcardResourceScope {
	if len(set) == 0 {
		return nil
	}

	out := make([]rls.WildcardResourceScope, 0, len(set))
	for val := range set {
		out = append(out, val)
	}

	sort.Slice(out, func(i, j int) bool {
		return string(out[i]) < string(out[j])
	})

	return out
}
