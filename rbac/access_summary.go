package rbac

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
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
var summaryObjects = map[string]string{
	policy.ResourceConfig:     policy.ObjectCatalog,
	policy.ResourceComponent:  policy.ObjectTopology,
	policy.ResourceCanary:     policy.ObjectCanary,
	policy.ResourcePlaybook:   policy.ObjectPlaybooks,
	policy.ResourceConnection: policy.ObjectConnection,
}

// summaryActions are the actions the access summary reports for each resource type.
var summaryActions = []string{policy.ActionRead, policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete}

// summaryCache holds summaries by subject and built-in roles. It's flushed whenever grants change.
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
// A grant on the whole type gives all, and a deny on part of it lowers that to some.
// Otherwise, grants that select only some resources of the type give some, even when they currently match no resource.
// A deny on the whole type gives none.
func AccessSummary(ctx context.Context) map[string]map[string]Access {
	user := ctx.User()
	if user == nil {
		return noAccess()
	}

	roles := builtInRoles(ctx)
	cacheKey := fmt.Sprintf("%s:%s", ctx.Subject(), strings.Join(roles, ","))
	if cached, ok := summaryCache.Get(cacheKey); ok {
		return cached.(map[string]map[string]Access)
	}
	generation := summaryGeneration.Load()

	guest := lo.Contains(roles, policy.RoleGuest)
	grants, err := selectorGrantsOf(ctx)
	if err != nil {
		ctx.Warnf("failed to load the grants of %s: %v", ctx.Subject(), err)
		return noAccess()
	}

	// Deny rules from Permissions and RoleBindings never apply to admins, built-in ones do
	if lo.Contains(roles, policy.RoleAdmin) {
		grants = lo.Filter(grants, func(g selectorGrant, _ int) bool { return !g.deny || g.builtIn })
	}

	summary := make(map[string]map[string]Access, len(summaryObjects))
	for resourceType, object := range summaryObjects {
		summary[resourceType] = make(map[string]Access, len(summaryActions))
		for _, action := range summaryActions {
			summary[resourceType][action] = accessTo(ctx, grants, resourceType, object, action, guest)
		}
	}

	summaryFlush.RLock()
	if summaryGeneration.Load() == generation {
		summaryCache.SetDefault(cacheKey, summary)
	}
	summaryFlush.RUnlock()
	return summary
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

func accessTo(ctx context.Context, grants []selectorGrant, resourceType, object, action string, guest bool) Access {
	applicable := lo.Filter(grants, func(g selectorGrant, _ int) bool { return g.action == action || g.action == "*" })
	denies := lo.Filter(applicable, func(g selectorGrant, _ int) bool { return g.deny })

	if lo.SomeBy(denies, func(g selectorGrant) bool { return g.deniesWhole(resourceType, object) }) {
		return AccessNone
	}

	partlyDenied := lo.SomeBy(denies, func(g selectorGrant) bool { return g.selects(resourceType) })
	if wholeType(ctx, object, action, guest) {
		return lo.Ternary(partlyDenied, AccessSome, AccessAll)
	}

	if hasSomeGrant(ctx, applicable, resourceType, action) {
		return AccessSome
	}
	return AccessNone
}

// wholeType reports whether the subject may act on every resource of the object.
// A guest passes whole-type read checks as a viewer only for its listings to be filtered by row,
// so only its own grants count.
func wholeType(ctx context.Context, object, action string, guest bool) bool {
	if guest {
		return rbac.Check(ctx, ctx.User().ID.String(), object, action)
	}
	return rbac.CheckContext(ctx, object, action)
}

// hasSomeGrant reports whether the subject has a grant that selects some resources of the type.
//
// Reads are granted by the row filters of read grants, the same ones its listings use. They only exist while
// row-level security is on, and never for connections, which can't be filtered by row.
func hasSomeGrant(ctx context.Context, grants []selectorGrant, resourceType, action string) bool {
	if action == policy.ActionRead {
		return ReadGrantsCover != nil && ReadGrantsCover(ctx, resourceType)
	}
	return lo.SomeBy(grants, func(g selectorGrant) bool { return !g.deny && g.selects(resourceType) })
}

// selectorGrant is an allow or deny of one action that the enforcer checks against the resources of each request,
// or a deny of a whole object.
type selectorGrant struct {
	action string
	deny   bool

	// object is set for a deny of a whole object, e.g. catalog, or of every object ("*").
	object string

	// builtIn is set for a policy that doesn't come from a Permission or a RoleBinding.
	builtIn bool

	// types maps each resource type the grant selects to whether it selects every resource of the type.
	types map[string]bool
}

func (g selectorGrant) selects(resourceType string) bool {
	_, ok := g.types[resourceType]
	return ok
}

func (g selectorGrant) deniesWhole(resourceType, object string) bool {
	if !g.deny {
		return false
	} else if g.object != "" {
		return g.object == object || g.object == "*"
	}
	return g.types[resourceType]
}

// selectorGrantsOf returns the grants of the subject that the enforcer checks against the resources of each request,
// and its denies of whole objects. Allows of whole objects aren't included: the enforcer answers for them directly.
func selectorGrantsOf(ctx context.Context) ([]selectorGrant, error) {
	subject := ctx.User().ID.String()
	if s := ctx.Subject(); !rbac.HasImplicitGrants(s) {
		subject = s
	}

	perms, err := rbac.PermsForUser(subject)
	if err != nil {
		return nil, err
	}

	// A person joins everyone on their first check, which may not have happened yet
	if rbac.HasImplicitGrants(subject) {
		everyone, err := rbac.PermsForUser(policy.RoleEveryone)
		if err != nil {
			return nil, err
		}
		perms = append(perms, everyone...)
	}

	permissionActions := map[uuid.UUID][]policy.Permission{}
	var grants []selectorGrant
	for _, perm := range perms {
		if perm.Condition == "" {
			if perm.Deny {
				grants = append(grants, selectorGrant{action: perm.Action, deny: true, object: perm.Object, builtIn: perm.ID == "" || perm.ID == "na"})
			}
			continue
		}

		// A policy with a condition only matches requests on resources, and those only match the object "*"
		if perm.Object != "*" {
			continue
		}

		// RoleBinding rules for the summarized actions only compile to conditions for read allows,
		// which the row filters already account for
		if id, err := uuid.Parse(perm.ID); err == nil {
			permissionActions[id] = append(permissionActions[id], perm)
		}
	}

	permissionGrants, err := permissionSelectorGrants(ctx, permissionActions)
	if err != nil {
		return nil, err
	}

	return append(grants, permissionGrants...), nil
}

// permissionSelectorGrants returns the grants of the given Permissions, for the actions their policies carry.
func permissionSelectorGrants(ctx context.Context, policies map[uuid.UUID][]policy.Permission) ([]selectorGrant, error) {
	if len(policies) == 0 {
		return nil, nil
	}

	var permissions []models.Permission
	if err := ctx.DB().Where("id IN ? AND deleted_at IS NULL", lo.Keys(policies)).Find(&permissions).Error; err != nil {
		return nil, fmt.Errorf("failed to load permissions: %w", err)
	}

	var grants []selectorGrant
	for _, permission := range permissions {
		selections, err := permissionSelections(ctx, permission)
		if adapter.IsValidationError(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to resolve permission %s: %w", permission.ID, err)
		}

		actions := lo.Uniq(lo.Map(policies[permission.ID], func(p policy.Permission, _ int) string { return p.Action }))
		for _, selected := range selections {
			for _, action := range actions {
				grants = append(grants, selectorGrant{action: action, deny: permission.Deny, types: selected})
			}
		}
	}
	return grants, nil
}

// permissionSelections returns what each policy of the Permission selects: one per target of the Scopes it references,
// or one for its own selectors.
//
// A policy only matches requests that carry every type it names, so a policy naming several types selects none of them
// on its own and isn't returned.
func permissionSelections(ctx context.Context, permission models.Permission) ([]map[string]bool, error) {
	objects, err := adapter.ExpandPermissionScopes(ctx, nil, permission)
	if err != nil {
		return nil, err
	} else if objects == nil {
		var object v1.PermissionObject
		if len(permission.ObjectSelector) > 0 {
			if err := json.Unmarshal(permission.ObjectSelector, &object); err != nil {
				return nil, adapter.NewValidationError("invalid object selector: %v", err)
			}
		}
		objects = []v1.PermissionObject{object}
	}

	ids := map[string]bool{
		policy.ResourceConfig:     permission.ConfigID != nil,
		policy.ResourceComponent:  permission.ComponentID != nil,
		policy.ResourceCanary:     permission.CanaryID != nil,
		policy.ResourcePlaybook:   permission.PlaybookID != nil,
		policy.ResourceConnection: permission.ConnectionID != nil,
	}

	var selections []map[string]bool
	for _, object := range objects {
		named := map[string][]types.ResourceSelector{
			policy.ResourceConfig:     object.Configs,
			policy.ResourceComponent:  object.Components,
			policy.ResourcePlaybook:   object.Playbooks,
			policy.ResourceConnection: object.Connections,
		}

		selected := map[string]bool{}
		for resourceType, selectors := range named {
			if len(selectors) > 0 {
				selected[resourceType] = !ids[resourceType] && lo.SomeBy(selectors, selectsAll)
			}
		}
		for resourceType, byID := range ids {
			if byID {
				selected[resourceType] = false
			}
		}
		if len(object.Views) > 0 {
			selected[policy.ResourceView] = false
		}

		if len(selected) == 1 {
			selections = append(selections, selected)
		}
	}
	return selections, nil
}

// selectsAll reports whether the selector matches every resource of its type.
func selectsAll(selector types.ResourceSelector) bool {
	return selector.Wildcard() || selector.IsEmpty()
}

// builtInRoles returns the sorted roles of the subject when it's a person.
// When the roles can't be read, it reports guest, so that only the subject's own grants count.
func builtInRoles(ctx context.Context) []string {
	user := ctx.User()
	if user == nil || !rbac.HasImplicitGrants(ctx.Subject()) {
		return nil
	}

	roles, err := rbac.RolesForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("failed to get roles of %s: %v", user.ID, err)
		return []string{policy.RoleGuest}
	}
	roles = lo.Uniq(roles)
	slices.Sort(roles)
	return roles
}
