package auth

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"

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

// rlsPayloadLeases versions the RLS payloads cached in tokenCache.
//
// Building a payload reads the subject's grants and takes a few queries. If the grants change and the cache is
// invalidated while a build is running, the invalidation finds nothing to delete and the build then caches
// the payload it built from the old grants, which keeps listing revoked rows until it expires an hour later.
//
// So a request takes a lease before it reads the grants, and caches the payload only if the lease is still valid.
// Invalidations void the leases.
type rlsPayloadLeases struct {
	sync.Mutex

	// all is bumped when every payload is invalidated.
	all uint64

	// bySubject is bumped when the subject's payloads are invalidated.
	bySubject map[string]uint64
}

var rlsLeases = rlsPayloadLeases{bySubject: map[string]uint64{}}

type rlsLease struct{ all, subject uint64 }

func takeRLSLease(subject string) rlsLease {
	rlsLeases.Lock()
	defer rlsLeases.Unlock()
	return rlsLease{rlsLeases.all, rlsLeases.bySubject[subject]}
}

// voidAllRLSLeases voids the leases of every subject. Call it before flushing the cached payloads.
func voidAllRLSLeases() {
	rlsLeases.Lock()
	defer rlsLeases.Unlock()
	rlsLeases.all++
	clear(rlsLeases.bySubject)
}

// InvalidateRLSCacheForUser removes the user's cached RLS payloads, including the ones
// cached per access token (<key>:token=<id>).
//
// Call it after the change to the user's grants is applied. It voids the user's leases, so requests that read the
// user's grants before the change don't cache what they built.
func InvalidateRLSCacheForUser(userID string) {
	rlsLeases.Lock()
	defer rlsLeases.Unlock()
	rlsLeases.bySubject[userID]++

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

	// Take the lease before reading grants: an invalidation while the payload is built voids it,
	// and the payload is then returned without being cached.
	lease := takeRLSLease(subject)

	payload, err := buildRLSPayload(ctx, subject)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	if tokenID, limited := rbac.TokenLimit(ctx, subject); limited {
		// A token lists the rows both its owner's read grants and its RoleBindings select (roles.md, Section 4.5)
		if payload, err = limitToTokenBindings(ctx, payload, tokenID); err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
	}

	// Cache the payload only if no invalidation voided the lease while it was built. The check and the write hold
	// the lock invalidations take, so an invalidation can't land between them.
	rlsLeases.Lock()
	if (rlsLease{rlsLeases.all, rlsLeases.bySubject[subject]}) == lease {
		tokenCache.SetDefault(cacheKey, payload)
	}
	rlsLeases.Unlock()
	return payload, nil
}

// limitToTokenBindings narrows the owner's row filters, built from the owner's read grants, to the rows the read
// rules of the token's RoleBindings list: a row is listed when a grant of the owner and a grant of a binding both
// admit it, and none of the owner's denies matches it. A binding that's invalid,
// or whose Role is missing or invalid, lists nothing.
func limitToTokenBindings(ctx context.Context, owner *rls.Payload, tokenID uuid.UUID) (*rls.Payload, error) {
	rules, err := adapter.LoadAccessTokenLimitRules(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to load the rules that limit access token %s: %w", tokenID, err)
	}

	limit := newClaim()
	for _, rule := range rules {
		limit.grantRule(rule)
	}

	result := &rls.Payload{}
	for _, kind := range rls.GrantTypes {
		ownerGrants := owner.GrantsFor(kind)
		if ownerGrants == nil || limit.grants[kind] == nil {
			continue
		}
		result.SetGrants(kind, intersectGrants(ownerGrants, limit.grants[kind]))
	}
	for _, kind := range rls.GrantTypes {
		for _, grant := range owner.DeniesFor(kind) {
			result.AddDeny(kind, grant)
		}
	}
	return result, nil
}

// intersectGrants returns the grants that admit the rows both a and b admit: one grant per pair of their grants,
// which a row must satisfy both of. b's Scopes are added to each of a's grants as constraints.
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
			narrowed.Constraints = append(append(slices.Clone(x.Constraints), y.Scope), y.Constraints...)
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
// binding's constraint.
//
// Casbin knows every grant that applies to the subject, directly or through roles, teams and bindings,
// and has already split each grant into one policy per action. Each policy carries the id of the
// binding and role rule it came from.
func buildRLSPayload(ctx context.Context, subject string) (*rls.Payload, error) {
	rules, err := dutyRBAC.PermsForUser(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to get permissions for user: %w", err)
	}

	bindingRules := map[uuid.UUID][]string{}
	for _, rule := range rules {
		if rule.Action != policy.ActionRead {
			continue
		}

		if bindingID, _, ok := adapter.ParseBindingRuleID(rule.ID); ok {
			bindingRules[bindingID] = append(bindingRules[bindingID], rule.ID)
		} else if rule.ID != "" && rule.ID != "na" { // built-in policies carry no id
			ctx.Warnf("rls: unrecognized id %q on policy %s %s %s", rule.ID, rule.Subject, rule.Object, rule.Action)
		}
	}

	claim := newClaim()
	if err := addBindingRuleGrants(ctx, claim, bindingRules); err != nil {
		return nil, err
	}
	if err := addBindingRuleDenies(ctx, claim, subject); err != nil {
		return nil, err
	}

	return claim.payload(), nil
}

// addBindingRuleDenies keeps from listings the rows the subject's deny RoleBindings deny reading. A rule that denies
// a whole type lists none of it. A binding that can't be compiled denies no rows, as it denies nothing else.
func addBindingRuleDenies(ctx context.Context, claim *claim, subject string) error {
	implicit, err := dutyRBAC.Enforcer().GetImplicitPermissionsForUser(subject)
	if err != nil {
		return fmt.Errorf("failed to get the denies of %s: %w", subject, err)
	}

	bindingRules := map[uuid.UUID][]string{}
	for _, rule := range implicit {
		if len(rule) < 6 || rule[2] != policy.ActionRead || rule[3] != "deny" {
			continue
		}
		if bindingID, _, ok := adapter.ParseBindingRuleID(rule[5]); ok {
			bindingRules[bindingID] = append(bindingRules[bindingID], rule[5])
		}
	}

	for bindingID, ids := range bindingRules {
		rules, err := adapter.LoadBindingRules(ctx, bindingID)
		if err != nil {
			if adapter.IsValidationError(err) {
				ctx.Warnf("rls: role binding %s denies no rows: %v", bindingID, err)
				continue
			}
			return fmt.Errorf("failed to load rules of role binding %s: %w", bindingID, err)
		}

		for _, rule := range rules {
			if rule.Deny && lo.Contains(ids, rule.ID) {
				claim.denyRule(rule)
			}
		}
	}

	return nil
}

// claim accumulates a subject's grants and denies by resource type.
type claim struct {
	grants map[string]*rls.Grants

	// denies are the rows kept from listings, by type. deniedTypes are the types none of whose rows are listed.
	denies      map[string][]rls.Grant
	deniedTypes map[string]bool
}

func newClaim() *claim {
	return &claim{grants: map[string]*rls.Grants{}, denies: map[string][]rls.Grant{}, deniedTypes: map[string]bool{}}
}

// denyRule keeps from listings the rows a deny read rule matches: every row of a type it selects entirely, and
// otherwise the rows in its Scopes.
func (c *claim) denyRule(rule adapter.CompiledRule) {
	for kind, grant := range rule.ReadGrants() {
		if rule.SelectsEntirely(kind) {
			c.deniedTypes[kind] = true
		}
		c.denies[kind] = append(c.denies[kind], grant)
	}
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
	if rule.Deny {
		return
	}

	for kind, grant := range rule.ReadGrants() {
		if kind == policy.ResourceScraper && rule.GrantsAllOf(kind) {
			c.grantAll(kind)
		} else {
			c.grant(kind, grant)
		}
	}
}

// payload returns the claim's row filters. A type denied whole has no grants, so it lists no rows.
// Denies are sent whether or not the type has grants: a row can be admitted another way, e.g. a check with its canary.
func (c *claim) payload() *rls.Payload {
	payload := &rls.Payload{}
	for _, kind := range rls.GrantTypes {
		if grants := c.grants[kind]; grants != nil && !c.deniedTypes[kind] {
			payload.SetGrants(kind, grants)
		}
		for _, grant := range c.denies[kind] {
			payload.AddDeny(kind, grant)
		}
	}
	return payload
}

// addBindingRuleGrants adds the grants of the given read rules (by id) of each binding.
// A rule narrowed by a constraint lists the rows in both Scopes. A rule on views lists every row of the views it
// grants, since a view is granted whole. A binding that can't be compiled grants no rows.
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
