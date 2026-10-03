package auth

import (
	"encoding/json"
	"fmt"
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

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/vars"
)

func init() {
	rbac.ReadGrantsCover = readGrantsCover
}

// readGrantsCover reports whether the row filters of the subject's read grants cover the resource type.
// While row-level security is off, nothing is filtered by row, so they cover nothing.
func readGrantsCover(ctx context.Context, resourceType string) bool {
	payload, err := GetRLSPayload(ctx)
	if err != nil {
		ctx.Errorf("failed to get row filters: %v", err)
		return false
	} else if payload.Disable {
		return false
	}

	switch resourceType {
	case policy.ResourceConfig:
		return len(payload.Config) > 0
	case policy.ResourceComponent:
		return len(payload.Component) > 0
	case policy.ResourceCheck:
		return len(payload.Check) > 0
	case policy.ResourceCanary:
		return len(payload.Canary) > 0
	case policy.ResourcePlaybook:
		return len(payload.Playbook) > 0
	}
	return false
}

func getRLSCacheKey(userID string) string {
	return fmt.Sprintf("rls-payload-%s", userID)
}

// InvalidateRLSCacheForUser removes the user's cached RLS payloads, including the ones
// cached per X-Flanksource-Scope header (<key>:<fingerprint>).
func InvalidateRLSCacheForUser(userID string) {
	cacheKey := getRLSCacheKey(userID)
	for key := range tokenCache.Items() {
		if key == cacheKey || strings.HasPrefix(key, cacheKey+":") {
			tokenCache.Delete(key)
		}
	}
}

func GetRLSPayload(ctx context.Context) (*rls.Payload, error) {
	if !vars.RLSEnabled(ctx) {
		return &rls.Payload{Disable: true}, nil
	}

	user := ctx.User()
	if user == nil {
		return nil, fmt.Errorf("user is required for RLS payload")
	}

	impersonated := getImpersonatedPayload(ctx)

	// Federated identities have no implicit grants: their row filters come from their own grants
	subject := user.ID.String()
	if s := ctx.Subject(); !dutyRBAC.HasImplicitGrants(s) {
		subject = s
	}

	cacheKey := getRLSCacheKey(subject)
	if impersonated != nil {
		cacheKey = fmt.Sprintf("%s:%s", cacheKey, impersonated.Fingerprint())
	}

	if cached, ok := tokenCache.Get(cacheKey); ok {
		return cached.(*rls.Payload), nil
	}

	// RLS is disabled for everyone but guests. Subjects without implicit grants are always filtered.
	if dutyRBAC.HasImplicitGrants(subject) {
		if roles, err := dutyRBAC.RolesForUser(user.ID.String()); err != nil {
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
	}

	// Build the row filters from the user's read grants
	payload, err := buildRLSPayload(ctx, subject)
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

// buildRLSPayload converts the subject's read grants into row filters (rls.Scope) that Postgres
// applies to every query, since casbin can't filter rows returned by /db.
//
// Casbin knows every grant that applies to the subject, directly or through roles, teams and bindings,
// and has already split each grant into one policy per action. Each policy carries the id of the
// Permission or the binding and role rule it came from, which hold the selectors the row filters are built from.
func buildRLSPayload(ctx context.Context, subject string) (*rls.Payload, error) {
	rules, err := dutyRBAC.PermsForUser(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to get permissions for user: %w", err)
	}

	var permissionIDs []string
	bindingRules := map[uuid.UUID][]string{}
	for _, rule := range rules {
		if rule.Action != policy.ActionRead {
			continue
		}

		if bindingID, _, ok := adapter.ParseBindingRuleID(rule.ID); ok {
			bindingRules[bindingID] = append(bindingRules[bindingID], rule.ID)
		} else if uuid.Validate(rule.ID) == nil {
			permissionIDs = append(permissionIDs, rule.ID)
		} else if rule.ID != "" && rule.ID != "na" { // built-in policies carry no id
			ctx.Warnf("rls: unrecognized id %q on policy %s %s %s", rule.ID, rule.Subject, rule.Object, rule.Action)
		}
	}

	payload := &rls.Payload{}
	if err := addPermissionFilters(ctx, payload, lo.Uniq(permissionIDs)); err != nil {
		return nil, err
	}
	if err := addBindingRuleFilters(ctx, payload, bindingRules); err != nil {
		return nil, err
	}

	return payload, nil
}

func addPermissionFilters(ctx context.Context, payload *rls.Payload, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	var permissions []models.Permission
	err := ctx.DB().
		Where("id IN ?", ids).
		Where("deleted_at IS NULL").
		Where("(object_selector IS NOT NULL) OR playbook_id IS NOT NULL OR canary_id IS NOT NULL OR component_id IS NOT NULL OR config_id IS NOT NULL OR connection_id IS NOT NULL").
		Find(&permissions).Error
	if err != nil {
		return fmt.Errorf("failed to query permissions: %w", err)
	}

	for _, perm := range permissions {
		if perm.ConfigID != nil {
			addConfigFilter(payload, rls.Scope{ID: perm.ConfigID.String()}, perm.Deny)
		}

		if perm.ComponentID != nil {
			addComponentFilter(payload, rls.Scope{ID: perm.ComponentID.String()}, perm.Deny)
		}

		if perm.PlaybookID != nil {
			addPlaybookFilter(payload, rls.Scope{ID: perm.PlaybookID.String()}, perm.Deny)
		}

		if perm.CanaryID != nil {
			addCanaryFilter(payload, rls.Scope{ID: perm.CanaryID.String()}, perm.Deny)
		}

		if len(perm.ObjectSelector) == 0 {
			continue
		}

		var selectors v1.PermissionObject
		if err := json.Unmarshal([]byte(perm.ObjectSelector), &selectors); err != nil {
			ctx.Warnf("failed to unmarshal object_selector for permission %s: %v", perm.ID, err)
			continue
		}

		if err := addObjectFilters(ctx, payload, selectors, perm.Deny); err != nil {
			return err
		}
	}

	return nil
}

// addBindingRuleFilters adds the row filters of the given read rules (by id) of each binding.
// A rule narrowed by a constraint filters the rows that match both Scopes.
//
// Role rules grant no rows of generated view tables: Views are outside the Role model.
// A binding that can't be compiled grants no rows.
func addBindingRuleFilters(ctx context.Context, payload *rls.Payload, bindingRules map[uuid.UUID][]string) error {
	for bindingID, ids := range bindingRules {
		rules, err := adapter.LoadBindingRules(ctx, bindingID)
		if err != nil {
			if adapter.IsValidationError(err) {
				ctx.Warnf("rls: role binding %s grants no rows: %v", bindingID, err)
				continue
			}
			return fmt.Errorf("failed to load rules of role binding %s: %w", bindingID, err)
		}

		for _, rule := range rules {
			if !lo.Contains(ids, rule.ID) || rule.Deny || rule.Contract.Action != policy.ActionRead {
				continue
			}

			filters, err := rule.RowFilters()
			if err != nil {
				return fmt.Errorf("failed to build row filters of role binding %s: %w", bindingID, err)
			}

			for kind, kindFilters := range filters {
				for _, filter := range kindFilters {
					switch kind {
					case policy.ResourceConfig:
						addConfigFilter(payload, filter, false)
					case policy.ResourceComponent:
						addComponentFilter(payload, filter, false)
					case policy.ResourceCheck:
						addCheckFilter(payload, filter, false)
					case policy.ResourcePlaybook:
						addPlaybookFilter(payload, filter, false)
					case policy.ResourceCanary:
						addCanaryFilter(payload, filter, false)
					}
				}
			}
		}
	}

	return nil
}

// addObjectFilters adds a row filter for each selector of the object
func addObjectFilters(ctx context.Context, payload *rls.Payload, selectors v1.PermissionObject, deny bool) error {
	// Permissions that reference Scope CRDs
	if len(selectors.Scopes) > 0 {
		if err := addScopeCRDFilters(ctx, selectors.Scopes, payload, deny); err != nil {
			return err
		}
	}

	// Direct resource selectors (configs, components, playbooks, etc.)
	// Only use tags, name, and agent_id as per requirements
	for _, selector := range selectors.Configs {
		addConfigFilter(payload, resourceSelectorToFilter(selector), deny)
	}

	for _, selector := range selectors.Components {
		addComponentFilter(payload, resourceSelectorToFilter(selector), deny)
	}

	for _, selector := range selectors.Playbooks {
		addPlaybookFilter(payload, resourceSelectorToFilter(selector), deny)
	}

	for _, viewRef := range selectors.Views {
		addViewFilter(payload, viewRefToFilter(viewRef), deny)
	}

	// TODO: No RLS support for connections yet!
	// if len(selectors.Connections) > 0 {
	// 	for _, selector := range selectors.Connections {
	// 		payload.Connections = append(payload.Connections, resourceSelectorToFilter(selector))
	// 	}
	// }

	return nil
}

func addConfigFilter(payload *rls.Payload, filter rls.Scope, deny bool) {
	filter.Deny = deny
	payload.Config = append(payload.Config, filter)
}

func addComponentFilter(payload *rls.Payload, filter rls.Scope, deny bool) {
	filter.Deny = deny
	payload.Component = append(payload.Component, filter)
}

func addPlaybookFilter(payload *rls.Payload, filter rls.Scope, deny bool) {
	filter.Deny = deny
	payload.Playbook = append(payload.Playbook, filter)
}

func addCanaryFilter(payload *rls.Payload, filter rls.Scope, deny bool) {
	filter.Deny = deny
	payload.Canary = append(payload.Canary, filter)
}

func addCheckFilter(payload *rls.Payload, filter rls.Scope, deny bool) {
	filter.Deny = deny
	payload.Check = append(payload.Check, filter)
}

func addViewFilter(payload *rls.Payload, filter rls.Scope, deny bool) {
	filter.Deny = deny
	payload.View = append(payload.View, filter)
}

// addScopeCRDFilters fetches the referenced Scope CRDs and adds their targets as row filters.
// A missing or invalid Scope selects nothing.
func addScopeCRDFilters(ctx context.Context, scopeRefs []dutyRBAC.NamespacedNameIDSelector, payload *rls.Payload, deny bool) error {
	for _, ref := range scopeRefs {
		scopeID, targets, err := adapter.LoadScope(ctx, nil, ref.Namespace, ref.Name)
		if err != nil {
			if adapter.IsValidationError(err) {
				ctx.Warnf("scope %s/%s selects nothing: %v", ref.Namespace, ref.Name, err)
				continue
			}
			return fmt.Errorf("failed to load scope %s/%s: %w", ref.Namespace, ref.Name, err)
		} else if targets == nil {
			ctx.Warnf("scope %s/%s not found", ref.Namespace, ref.Name)
			continue
		}

		// Add scope UUID for view row-level grants
		if !deny {
			payload.Scopes = append(payload.Scopes, scopeID)
		}

		for _, target := range targets {
			_, selector := target.Selector()
			filter := resourceSelectorToFilter(selector)
			if target.Config != nil {
				addConfigFilter(payload, filter, deny)
			}
			if target.Component != nil {
				addComponentFilter(payload, filter, deny)
			}
			if target.Playbook != nil {
				addPlaybookFilter(payload, filter, deny)
			}
			if target.Canary != nil {
				addCanaryFilter(payload, filter, deny)
			}
			if target.View != nil {
				addViewFilter(payload, filter, deny)
			}
		}
	}

	return nil
}

// resourceSelectorToFilter converts a types.ResourceSelector to a row filter
// Only uses tags, name, and agent_id.
func resourceSelectorToFilter(selector types.ResourceSelector) rls.Scope {
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

// viewRefToFilter converts a view ViewRef (namespace/name) to a row filter
// Views only support id and name in match_scope (namespace is not supported)
func viewRefToFilter(viewRef dutyRBAC.ViewRef) rls.Scope {
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
