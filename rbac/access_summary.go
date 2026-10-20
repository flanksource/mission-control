package rbac

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// Access is how much of a resource type a subject may act on.
type Access string

const (
	AccessAll  Access = "all"
	AccessSome Access = "some"
	AccessNone Access = "none"
)

// summaryObjects are the resource types the access summary reports, with the RBAC object an action on the whole type is granted on.
var summaryObjects = func() map[string]string {
	contract, _ := adapter.ContractFor(policy.ActionRead)
	objects := make(map[string]string, len(contract.Resources))
	for _, resourceType := range contract.Resources {
		objects[resourceType] = adapter.ObjectForType(resourceType)
	}
	return objects
}()

// summaryActions are the actions the access summary reports for each resource type.
var summaryActions = []string{policy.ActionRead, policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete}

// summaryCache holds summaries by subject, the roles, e.g. teams and bindings, it's assigned to, and the access token
// the request was made with. It's flushed whenever grants change.
var summaryCache = gocache.New(10*time.Minute, 10*time.Minute)

// summaryGeneration counts flushes, so a summary computed across a flush isn't cached.
// summaryFlush keeps a flush from landing between that check and the write.
var (
	summaryGeneration atomic.Uint64
	summaryFlush      sync.RWMutex
)

// FlushAccessSummaries drops every cached access summary. Call it whenever grants change.
func FlushAccessSummaries() {
	summaryFlush.Lock()
	defer summaryFlush.Unlock()
	summaryGeneration.Add(1)
	summaryCache.Flush()
}

// AccessSummary reports, for each resource type and action, whether the subject may act on all, some or none of it.
//
// A grant on the whole type gives all.
// Otherwise, grants that select only some resources of the type give some, even when they currently match no resource.
func AccessSummary(ctx context.Context) map[string]map[string]Access {
	user := ctx.User()
	if user == nil {
		return noAccess()
	}

	roles := subjectRoles(ctx)
	cacheKey := fmt.Sprintf("%s:%s%s", ctx.Subject(), strings.Join(roles, ","), AccessTokenCacheKey(ctx))
	if cached, ok := summaryCache.Get(cacheKey); ok {
		return cached.(map[string]map[string]Access)
	}
	generation := summaryGeneration.Load()

	summary := make(map[string]map[string]Access, len(summaryObjects))
	for resourceType, object := range summaryObjects {
		summary[resourceType] = make(map[string]Access, len(summaryActions))
		for _, action := range summaryActions {
			summary[resourceType][action] = accessTo(ctx, resourceType, object, action)
		}
	}

	if tokenID, limited := TokenLimit(ctx, ctx.Subject()); limited {
		if err := limitSummary(ctx, summary, ctx.Subject(), tokenID); err != nil {
			ctx.Warnf("failed to limit the access summary of %s to its token's role bindings: %v", ctx.Subject(), err)
			return noAccess()
		}
	}

	summaryFlush.RLock()
	if summaryGeneration.Load() == generation {
		summaryCache.SetDefault(cacheKey, summary)
	}
	summaryFlush.RUnlock()
	return summary
}

// limitSummary lowers each entry of the summary to what the token's RoleBindings allow: all when their rules match
// every resource of the type, some when they select some of them, and none otherwise.
func limitSummary(ctx context.Context, summary map[string]map[string]Access, subject string, tokenID uuid.UUID) error {
	rules, err := adapter.LoadAccessTokenLimitRules(ctx, tokenID)
	if err != nil {
		return err
	}

	for resourceType, object := range summaryObjects {
		for _, action := range summaryActions {
			limit := AccessNone
			if TokenAllowsObject(ctx, subject, object, action) {
				limit = AccessAll
			} else if lo.SomeBy(rules, func(rule adapter.CompiledRule) bool {
				return rule.Contract.Action == action && len(rule.Resource) > 0 &&
					lo.Contains(rule.Resource[0].DeclaredTypes(), resourceType)
			}) {
				limit = AccessSome
			}

			summary[resourceType][action] = leastAccess(summary[resourceType][action], limit)
		}
	}
	return nil
}

func leastAccess(a, b Access) Access {
	if a == AccessNone || b == AccessNone {
		return AccessNone
	} else if a == AccessAll && b == AccessAll {
		return AccessAll
	}
	return AccessSome
}

func noAccess() map[string]map[string]Access {
	summary := make(map[string]map[string]Access, len(summaryObjects))
	for resourceType := range summaryObjects {
		summary[resourceType] = make(map[string]Access, len(summaryActions))
		for _, action := range summaryActions {
			summary[resourceType][action] = AccessNone
		}
	}
	return summary
}

// accessTo reports how much of the type the subject may act on. Only reads can be granted on some resources of a
// type: they're granted by the row filters of read grants, the same ones its listings use. They never exist for
// connections, which can't be filtered by row.
func accessTo(ctx context.Context, resourceType, object, action string) Access {
	contract, err := adapter.ContractFor(action)
	if err != nil || !slices.Contains(contract.Resources, resourceType) {
		return AccessNone
	}
	if rbac.Check(ctx, ctx.Subject(), object, action) {
		return AccessAll
	}

	if action == policy.ActionRead && ReadGrantsCover != nil && ReadGrantsCover(ctx, resourceType) {
		return AccessSome
	}
	return AccessNone
}

// AccessActions reports resource-free actions as the caller, intersected with its token's Roles.
func AccessActions(ctx context.Context) map[string]bool {
	actions := map[string]bool{}
	for _, action := range adapter.Actions() {
		contract, _ := adapter.ContractFor(action)
		if !contract.TakesResource() {
			actions[action] = CheckContext(ctx, contract.Object, action)
		}
	}
	return actions
}

// subjectRoles returns the sorted roles, e.g. teams and bindings, the subject is assigned to.
func subjectRoles(ctx context.Context) []string {
	roles, err := rbac.RolesForUser(ctx.Subject())
	if err != nil {
		ctx.Warnf("failed to get roles of %s: %v", ctx.Subject(), err)
		return nil
	}
	roles = lo.Uniq(roles)
	slices.Sort(roles)
	return roles
}
