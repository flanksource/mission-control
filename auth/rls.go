package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	"github.com/lib/pq"
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
		return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
			role := dutyAPI.DefaultConfig.Postgrest.DBRoleBypass
			if role == "" {
				role = dutyAPI.DefaultConfig.Postgrest.DBRole
				if role != "" {
					txCtx.Logger.Warnf("RLS bypass role not configured, using role=%s", role)
				}
			}
			if role == "" {
				return fmt.Errorf("role is required")
			}
			if err := txCtx.DB().Exec(fmt.Sprintf("SET LOCAL ROLE %s", pq.QuoteIdentifier(role))).Error; err != nil {
				return err
			}
			return fn(txCtx)
		})
	}

	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		if err := rlsPayload.SetPostgresSessionRLSWithRole(txCtx.DB(), dutyAPI.DefaultConfig.Postgrest.DBRole); err != nil {
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

	for _, perm := range permissions {
		if !collections.MatchItems(policy.ActionRead, strings.Split(perm.Action, ",")...) {
			continue
		}

		permScopeID := perm.ID

		if perm.ConfigID != nil || perm.ComponentID != nil || perm.PlaybookID != nil || perm.CanaryID != nil {
			scopeIDs[permScopeID] = struct{}{}
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
			if err := processScopeRefs(ctx, selectors.Scopes, scopeIDs); err != nil {
				return nil, err
			}
		}

		// Process direct resource selectors (configs, components, playbooks, etc.)
		if len(selectors.Configs) > 0 {
			scopeIDs[permScopeID] = struct{}{}
		}

		if len(selectors.Components) > 0 {
			scopeIDs[permScopeID] = struct{}{}
		}

		if len(selectors.Playbooks) > 0 {
			scopeIDs[permScopeID] = struct{}{}
		}

		if len(selectors.Views) > 0 {
			scopeIDs[permScopeID] = struct{}{}
		}

		// TODO: No RLS support for connections yet!
		// if len(selectors.Connections) > 0 {
		// 	for _, selector := range selectors.Connections {
		// 		payload.Connections = append(payload.Connections, convertResourceSelectorToRLSScope(selector))
		// 	}
		// }
	}

	payload := &rls.Payload{
		Scopes: setToSortedUUIDSlice(scopeIDs),
	}

	return payload, nil
}

// processScopeRefs fetches scopes from database and adds their IDs
func processScopeRefs(ctx context.Context, scopeRefs []dutyRBAC.NamespacedNameIDSelector, scopeIDs map[uuid.UUID]struct{}) error {
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

	}

	return nil
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
