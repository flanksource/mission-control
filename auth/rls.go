package auth

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
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

// readGrantsCover reports whether the subject's claim filters the rows of the resource type.
// A deny Permission on read leaves a type with no grants, which still filters it: its listing is empty, not refused.
// While row-level security is off, nothing is filtered by row, so they cover nothing.
func readGrantsCover(ctx context.Context, resourceType string) bool {
	payload, err := GetRLSPayload(ctx)
	if err != nil {
		ctx.Errorf("failed to get row filters: %v", err)
		return false
	} else if payload.Disable {
		return false
	}

	return payload.GrantsFor(resourceType) != nil
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

	impersonated := getImpersonatedScopes(ctx)

	// Federated identities have no implicit grants: their row filters come from their own grants
	subject := user.ID.String()
	if s := ctx.Subject(); !dutyRBAC.HasImplicitGrants(s) {
		subject = s
	}

	cacheKey := getRLSCacheKey(subject)
	if impersonated != nil {
		cacheKey = fmt.Sprintf("%s:scopes=%s", cacheKey, strings.Join(impersonated, ","))
	}

	if cached, ok := tokenCache.Get(cacheKey); ok {
		return cached.(*rls.Payload), nil
	}

	// RLS is disabled for everyone but guests. Subjects without implicit grants are always filtered.
	if dutyRBAC.HasImplicitGrants(subject) {
		if roles, err := dutyRBAC.RolesForUser(user.ID.String()); err != nil {
			return nil, err
		} else if !lo.Contains(roles, policy.RoleGuest) {
			payload := applyImpersonation(&rls.Payload{Disable: true}, impersonated)
			tokenCache.SetDefault(cacheKey, payload)
			return payload, nil
		}
	}

	// Build the row filters from the user's read grants
	payload, err := buildRLSPayload(ctx, subject)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	payload = applyImpersonation(payload, impersonated)
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

// buildRLSPayload converts the subject's read grants into the claim Postgres filters listings by,
// since casbin can't filter rows returned by /db.
//
// Each type's grants name Scopes only: a row is listed when it's in every Scope of one of them,
// which Postgres reads from stored membership. Role rules grant rows through their Scopes, narrowed by their
// binding's constraint. Permissions grant rows only through the Scopes they name: inline selectors grant none.
// A deny Permission on read can't be enforced row by row, so the types it covers list no rows.
//
// Casbin knows every grant that applies to the subject, directly or through roles, teams and bindings,
// and has already split each grant into one policy per action. Each policy carries the id of the
// Permission or the binding and role rule it came from.
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

	claim := newClaim()
	if err := addPermissionGrants(ctx, claim, lo.Uniq(permissionIDs)); err != nil {
		return nil, err
	}
	if err := addBindingRuleGrants(ctx, claim, bindingRules); err != nil {
		return nil, err
	}

	return claim.payload(), nil
}

// claim accumulates a subject's grants by resource type.
type claim struct {
	grants map[string]*rls.Grants
	denied map[string]bool
	view   []rls.Scope
	scopes []string
}

func newClaim() *claim {
	return &claim{grants: map[string]*rls.Grants{}, denied: map[string]bool{}}
}

// grant lists the rows of the type that the grant admits.
func (c *claim) grant(kind string, grant rls.Grant) {
	if !lo.Contains(rls.GrantTypes, kind) {
		return
	}
	if c.grants[kind] == nil {
		c.grants[kind] = rls.NoRows()
	}
	c.grants[kind].Add(grant)
}

// deny lists no rows of the type, whatever else grants them.
func (c *claim) deny(kind string) {
	if lo.Contains(rls.GrantTypes, kind) {
		c.denied[kind] = true
	}
}

func (c *claim) payload() *rls.Payload {
	payload := &rls.Payload{View: c.view}
	if len(c.scopes) > 0 {
		payload.Scopes = lo.Uniq(c.scopes)
	}
	for _, kind := range rls.GrantTypes {
		if c.denied[kind] {
			payload.SetGrants(kind, rls.NoRows())
		} else if grants := c.grants[kind]; grants != nil {
			payload.SetGrants(kind, grants)
		}
	}
	return payload
}

// typesOfObject are the resource types a Permission on a global object covers.
var typesOfObject = map[string][]string{
	policy.ObjectCatalog:   {policy.ResourceConfig},
	policy.ObjectTopology:  {policy.ResourceComponent},
	policy.ObjectCanary:    {policy.ResourceCanary, policy.ResourceCheck},
	policy.ObjectPlaybooks: {policy.ResourcePlaybook},
}

func addPermissionGrants(ctx context.Context, claim *claim, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	var permissions []models.Permission
	err := ctx.DB().
		Where("id IN ?", ids).
		Where("deleted_at IS NULL").
		Find(&permissions).Error
	if err != nil {
		return fmt.Errorf("failed to query permissions: %w", err)
	}

	for _, perm := range permissions {
		if perm.Deny {
			for _, kind := range typesOfObject[perm.Object] {
				claim.deny(kind)
			}
			if perm.ConfigID != nil {
				claim.deny(policy.ResourceConfig)
			}
			if perm.ComponentID != nil {
				claim.deny(policy.ResourceComponent)
			}
			if perm.PlaybookID != nil {
				claim.deny(policy.ResourcePlaybook)
			}
			if perm.CanaryID != nil {
				claim.deny(policy.ResourceCanary)
			}
		}

		if len(perm.ObjectSelector) == 0 {
			continue
		}

		var selectors v1.PermissionObject
		if err := json.Unmarshal([]byte(perm.ObjectSelector), &selectors); err != nil {
			ctx.Warnf("failed to unmarshal object_selector for permission %s: %v", perm.ID, err)
			continue
		}

		if err := addObjectGrants(ctx, claim, selectors, perm.Deny); err != nil {
			return err
		}
	}

	return nil
}

// addBindingRuleGrants adds the grants of the given read rules (by id) of each binding.
// A rule narrowed by a constraint lists the rows in both Scopes.
//
// Role rules grant no rows of generated view tables: Views are outside the Role model.
// A binding that can't be compiled grants no rows.
func addBindingRuleGrants(ctx context.Context, claim *claim, bindingRules map[uuid.UUID][]string) error {
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
			if !lo.Contains(ids, rule.ID) {
				continue
			}

			for kind, grant := range rule.ReadGrants() {
				claim.grant(kind, grant)
			}
		}
	}

	return nil
}

// addObjectGrants adds what a Permission's object grants on listings: the rows of the Scopes it names.
// Its inline selectors grant no rows, except of views, which are still filtered by their selectors.
// A deny covers every type its selectors or Scopes select.
func addObjectGrants(ctx context.Context, claim *claim, selectors v1.PermissionObject, deny bool) error {
	if len(selectors.Scopes) > 0 {
		if err := addScopeGrants(ctx, selectors.Scopes, claim, deny); err != nil {
			return err
		}
	}

	if deny {
		if len(selectors.Configs) > 0 {
			claim.deny(policy.ResourceConfig)
		}
		if len(selectors.Components) > 0 {
			claim.deny(policy.ResourceComponent)
		}
		if len(selectors.Playbooks) > 0 {
			claim.deny(policy.ResourcePlaybook)
		}
	}

	for _, viewRef := range selectors.Views {
		addViewFilter(claim, viewRefToFilter(viewRef), deny)
	}

	return nil
}

func addViewFilter(claim *claim, filter rls.Scope, deny bool) {
	filter.Deny = deny
	claim.view = append(claim.view, filter)
}

// addScopeGrants grants the rows of the referenced Scopes, of each type they select.
// A missing or invalid Scope selects nothing.
func addScopeGrants(ctx context.Context, scopeRefs []dutyRBAC.NamespacedNameIDSelector, claim *claim, deny bool) error {
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
			claim.scopes = append(claim.scopes, scopeID)
		}

		for _, target := range targets {
			kind, selector := target.Selector()
			if target.View != nil {
				addViewFilter(claim, viewRefToFilter(dutyRBAC.ViewRef{ID: selector.ID, Name: selector.Name, Namespace: selector.Namespace}), deny)
			} else if deny {
				claim.deny(kind)
			} else {
				claim.grant(kind, rls.Grant{Scope: scopeID})
			}
		}
	}

	return nil
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

	return rlsScope
}
