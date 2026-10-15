package auth

import (
	"encoding/json"
	"fmt"
	"slices"
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
)

func init() {
	rbac.ReadGrantsCover = readGrantsCover
}

// readGrantsCover reports whether the subject's claim filters the rows of the resource type.
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
// cached per access token (<key>:token=<id>).
func InvalidateRLSCacheForUser(userID string) {
	cacheKey := getRLSCacheKey(userID)
	for key := range tokenCache.Items() {
		if key == cacheKey || strings.HasPrefix(key, cacheKey+":") {
			tokenCache.Delete(key)
		}
	}
}

// invalidateRLSCacheForToken removes the RLS payloads cached for requests made with the access token.
func invalidateRLSCacheForToken(tokenID uuid.UUID) {
	key := rbac.AccessTokenIDCacheKey(tokenID)
	for cacheKey := range tokenCache.Items() {
		if strings.HasPrefix(cacheKey, "rls-payload-") && strings.Contains(cacheKey, key) {
			tokenCache.Delete(cacheKey)
		}
	}
}

func GetRLSPayload(ctx context.Context) (*rls.Payload, error) {
	user := ctx.User()
	if user == nil {
		return nil, fmt.Errorf("user is required for RLS payload")
	}

	subject := ctx.Subject()

	// The subject's sessions and each of their access tokens are limited differently
	cacheKey := getRLSCacheKey(subject) + rbac.AccessTokenCacheKey(ctx)

	if cached, ok := tokenCache.Get(cacheKey); ok {
		return cached.(*rls.Payload), nil
	}

	var payload *rls.Payload
	var err error
	if tokenRoles, limited := rbac.TokenLimit(ctx, subject); limited {
		// A token lists the rows both its owner's read grants and its Roles select (roles.md, Section 4.5)
		if payload, err = buildRLSPayload(ctx, subject); err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
		if payload, err = limitToTokenRoles(ctx, payload, tokenRoles); err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
	} else if payload, err = buildRLSPayload(ctx, subject); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	tokenCache.SetDefault(cacheKey, payload)
	return payload, nil
}

// limitToTokenRoles narrows the owner's row filters, built from the owner's read grants, to the rows the read rules
// of the token's Roles list: a row is listed when a grant of the owner and a grant of a Role both admit it. Roles list
// no view rows, as Role rules don't for bindings. A Role that's missing or invalid lists nothing.
func limitToTokenRoles(ctx context.Context, owner *rls.Payload, roles models.AccessTokenRoles) (*rls.Payload, error) {
	limit := newClaim()
	for _, role := range roles {
		rules, err := adapter.LoadRoleLimitRules(ctx, role.Namespace, role.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to load the rules of role %s/%s: %w", role.Namespace, role.Name, err)
		}
		for _, rule := range rules {
			limit.grantRule(rule)
		}
	}

	result := &rls.Payload{}
	for _, kind := range rls.GrantTypes {
		ownerGrants := owner.GrantsFor(kind)
		if ownerGrants == nil || limit.grants[kind] == nil {
			continue
		}
		result.SetGrants(kind, intersectGrants(ownerGrants, limit.grants[kind]))
	}
	return result, nil
}

// intersectGrants returns the grants that admit the rows both a and b admit: one grant per pair of their grants,
// which a row must satisfy both of. b's Scopes are added to each of a's grants as Scopes the row must also be in.
func intersectGrants(a, b *rls.Grants) *rls.Grants {
	if a.All {
		return &rls.Grants{All: b.All, Any: slices.Clone(b.Any)}
	} else if b.All {
		return &rls.Grants{Any: slices.Clone(a.Any)}
	}

	result := rls.NoRows()
	for _, x := range a.Any {
		for _, y := range b.Any {
			narrowed := x
			narrowed.Impersonated = append(slices.Clone(x.Impersonated), y.Impersonated...)
			for _, id := range []string{y.Scope, y.Constraint} {
				if id != "" {
					narrowed.Impersonated = append(narrowed.Impersonated, id)
				}
			}
			result.Add(narrowed)
		}
	}
	return result
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

// CanReadArtifact reports whether the artifact exists and is visible under the subject's row filters, e.g. an
// artifact a scraper produced only when the subject can list the scraper.
func CanReadArtifact(ctx context.Context, id uuid.UUID) (bool, error) {
	var count int64
	err := WithRLS(ctx, func(ctx context.Context) error {
		return ctx.DB().Table("artifacts").Where("id = ? AND deleted_at IS NULL", id).Count(&count).Error
	})
	return count > 0, err
}

// buildRLSPayload converts the subject's read grants into the claim Postgres filters listings by,
// since casbin can't filter rows returned by /db.
//
// Each type's grants name Scopes only: a row is listed when it's in every Scope of one of them,
// which Postgres reads from stored membership. Role rules grant rows through their Scopes, narrowed by their
// binding's constraint. Permissions grant rows only through the Scopes they name: inline selectors grant none.
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
	view   []rls.Scope
	scopes []string
}

func newClaim() *claim {
	return &claim{grants: map[string]*rls.Grants{}}
}

// grant lists the rows of the type that the grant admits.
func (c *claim) grant(kind string, grant rls.Grant) {
	if !lo.Contains(rls.GrantTypes, kind) {
		return
	}
	if c.grants[kind] == nil {
		c.grants[kind] = rls.NoRows()
	} else if c.grants[kind].All {
		return
	}
	c.grants[kind].Add(grant)
}

// grantAll lists every row of the type.
func (c *claim) grantAll(kind string) {
	if lo.Contains(rls.GrantTypes, kind) {
		c.grants[kind] = rls.AllRows()
	}
}

// grantRule lists the rows a read rule grants. A rule over all scrapers through shipped Scopes lists every scraper
// as "all". Other grants name their Scopes, so a cached claim follows changes to the Scopes' membership.
func (c *claim) grantRule(rule adapter.CompiledRule) {
	for kind, grant := range rule.ReadGrants() {
		if kind == policy.ResourceScraper && rule.GrantsAllOf(kind) {
			c.grantAll(kind)
		} else {
			c.grant(kind, grant)
		}
	}
}

func (c *claim) payload() *rls.Payload {
	payload := &rls.Payload{View: c.view}
	if len(c.scopes) > 0 {
		payload.Scopes = lo.Uniq(c.scopes)
	}
	for _, kind := range rls.GrantTypes {
		if grants := c.grants[kind]; grants != nil {
			payload.SetGrants(kind, grants)
		}
	}
	return payload
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
		if len(perm.ObjectSelector) == 0 {
			continue
		}

		var selectors v1.PermissionObject
		if err := json.Unmarshal([]byte(perm.ObjectSelector), &selectors); err != nil {
			ctx.Warnf("failed to unmarshal object_selector for permission %s: %v", perm.ID, err)
			continue
		}

		if err := addObjectGrants(ctx, claim, selectors); err != nil {
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
			if lo.Contains(ids, rule.ID) {
				claim.grantRule(rule)
			}
		}
	}

	return nil
}

// addObjectGrants adds what a Permission's object grants on listings: the rows of the Scopes it names.
// Its inline selectors grant no rows, except of views, which are still filtered by their selectors.
func addObjectGrants(ctx context.Context, claim *claim, selectors v1.PermissionObject) error {
	if len(selectors.Scopes) > 0 {
		if err := addScopeGrants(ctx, selectors.Scopes, claim); err != nil {
			return err
		}
	}

	for _, viewRef := range selectors.Views {
		claim.view = append(claim.view, viewRefToFilter(viewRef))
	}

	return nil
}

// addScopeGrants grants the rows of the referenced Scopes, of each type they select.
// A missing or invalid Scope selects nothing.
func addScopeGrants(ctx context.Context, scopeRefs []dutyRBAC.NamespacedNameIDSelector, claim *claim) error {
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
		claim.scopes = append(claim.scopes, scopeID)

		for _, target := range targets {
			kind, selector := target.Selector()
			if target.View != nil {
				claim.view = append(claim.view, viewRefToFilter(dutyRBAC.ViewRef{ID: selector.ID, Name: selector.Name, Namespace: selector.Namespace}))
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
