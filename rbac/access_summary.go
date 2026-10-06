package rbac

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
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

// AccessSummary reports, for each resource type and action, whether the subject may act on all, some or none of it.
//
// A grant on the whole type gives all, and a deny on part of it lowers that to some.
// Otherwise, grants that select only some resources of the type give some, even when they currently match no resource.
// A deny on the whole type gives none.
//
// Deny rules never apply to admins, so an admin may act on all of every type.
func AccessSummary(ctx context.Context) map[string]map[string]Access {
	roles := builtInRoles(ctx)
	guest := lo.Contains(roles, policy.RoleGuest)

	var grants []selectorGrant
	if !lo.Contains(roles, policy.RoleAdmin) {
		grants = selectorGrantsOf(ctx)
	}

	summary := make(map[string]map[string]Access, len(summaryObjects))
	for resourceType, object := range summaryObjects {
		summary[resourceType] = make(map[string]Access, len(summaryActions))
		for _, action := range summaryActions {
			summary[resourceType][action] = accessTo(ctx, grants, resourceType, object, action, guest)
		}
	}

	return summary
}

func accessTo(ctx context.Context, grants []selectorGrant, resourceType, object, action string, guest bool) Access {
	if deniesWhole(grants, resourceType, object, action) {
		return AccessNone
	}

	partlyDenied := lo.SomeBy(grants, func(g selectorGrant) bool { return g.deny && g.selects(resourceType, action) })
	if wholeType(ctx, object, action, guest) {
		return lo.Ternary(partlyDenied, AccessSome, AccessAll)
	}

	if hasSomeGrant(ctx, grants, resourceType, action) {
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
// Reads are granted by the row filters of read grants, which only exist while row-level security is on.
func hasSomeGrant(ctx context.Context, grants []selectorGrant, resourceType, action string) bool {
	if action == policy.ActionRead {
		return ReadGrantsCover != nil && ReadGrantsCover(ctx, resourceType)
	}
	return lo.SomeBy(grants, func(g selectorGrant) bool { return !g.deny && g.selects(resourceType, action) })
}

// deniesWhole reports whether a deny covers every resource of the type: one on the object itself,
// or one whose selector matches every resource of the type.
func deniesWhole(grants []selectorGrant, resourceType, object, action string) bool {
	return lo.SomeBy(grants, func(g selectorGrant) bool {
		if !g.deny || !actionMatches(action, g.action) {
			return false
		}
		if g.types == nil {
			return g.object == object || g.object == "*"
		}
		return g.types[resourceType]
	})
}

// selectorGrant is a casbin policy of the subject, with the resource types its condition selects.
type selectorGrant struct {
	object string
	action string
	deny   bool

	// types maps each resource type the condition selects to whether it selects every resource of the type.
	// Nil for a policy without a condition, which applies to its object as a whole.
	types map[string]bool
}

func (g selectorGrant) selects(resourceType, action string) bool {
	if g.types == nil || !actionMatches(action, g.action) {
		return false
	}
	_, ok := g.types[resourceType]
	return ok
}

func actionMatches(action, policyAction string) bool {
	return policyAction == "*" || policyAction == action
}

// selectorGrantsOf returns the subject's policies, with the resource types each condition selects.
func selectorGrantsOf(ctx context.Context) []selectorGrant {
	user := ctx.User()
	if user == nil {
		return nil
	}

	subject := user.ID.String()
	if s := ctx.Subject(); !rbac.HasImplicitGrants(s) {
		subject = s
	}

	perms, err := rbac.PermsForUser(subject)
	if err != nil {
		ctx.Warnf("failed to get permissions of %s: %v", subject, err)
		return nil
	}

	grants := make([]selectorGrant, 0, len(perms))
	for _, perm := range perms {
		grant := selectorGrant{object: perm.Object, action: perm.Action, deny: perm.Deny}
		if perm.Condition != "" {
			grant.types = conditionTypes(perm.Condition)
		}
		grants = append(grants, grant)
	}
	return grants
}

var idConditionTypes = map[string]string{
	"str(r.obj.Config.ID)":     policy.ResourceConfig,
	"str(r.obj.Component.ID)":  policy.ResourceComponent,
	"str(r.obj.Canary.ID)":     policy.ResourceCanary,
	"str(r.obj.Playbook.ID)":   policy.ResourcePlaybook,
	"str(r.obj.Connection.ID)": policy.ResourceConnection,
}

// conditionTypes returns the resource types a policy condition selects, and whether it selects every resource of each.
// The conditions are the ones Permissions and RoleBindings compile to.
func conditionTypes(condition string) map[string]bool {
	selected := map[string]bool{}
	for _, clause := range strings.Split(condition, " && ") {
		if arg, ok := conditionArg(clause, "matchRule(r.obj, "); ok {
			var rule rbac.RuleCondition
			if json.Unmarshal([]byte(arg), &rule) == nil {
				addRuleTypes(selected, rule)
			}
		} else if arg, ok := conditionArg(clause, "matchResourceSelector(r.obj, "); ok {
			var selectors rbac.Selectors
			if json.Unmarshal([]byte(arg), &selectors) == nil {
				addSelectorTypes(selected, policy.ResourceConfig, selectors.Configs)
				addSelectorTypes(selected, policy.ResourceComponent, selectors.Components)
				addSelectorTypes(selected, policy.ResourcePlaybook, selectors.Playbooks)
				addSelectorTypes(selected, policy.ResourceConnection, selectors.Connections)
			}
		} else if field, _, ok := strings.Cut(clause, " == "); ok {
			if resourceType, ok := idConditionTypes[field]; ok {
				if _, ok := selected[resourceType]; !ok {
					selected[resourceType] = false
				}
			}
		}
	}
	return selected
}

// conditionArg returns the unquoted argument of a call to a condition function.
func conditionArg(clause, prefix string) (string, bool) {
	quoted, ok := strings.CutPrefix(clause, prefix)
	if !ok {
		return "", false
	}
	arg, err := strconv.Unquote(strings.TrimSuffix(quoted, ")"))
	return arg, err == nil
}

// addRuleTypes adds the resource types a compiled Role rule selects: those every Scope it must belong to selects.
func addRuleTypes(selected map[string]bool, rule rbac.RuleCondition) {
	if len(rule.Resource) == 0 {
		return
	}
	for _, resourceType := range rule.ResourceTypes {
		if !lo.EveryBy(rule.Resource, func(r rbac.RuleResources) bool { return len(r[resourceType]) > 0 }) {
			continue
		}
		whole := lo.EveryBy(rule.Resource, func(r rbac.RuleResources) bool { return lo.SomeBy(r[resourceType], types.ResourceSelector.Wildcard) })
		selected[resourceType] = selected[resourceType] || whole
	}
}

func addSelectorTypes(selected map[string]bool, resourceType string, selectors []types.ResourceSelector) {
	if len(selectors) == 0 {
		return
	}
	selected[resourceType] = selected[resourceType] || lo.SomeBy(selectors, types.ResourceSelector.Wildcard)
}

// builtInRoles returns the roles of the subject when it's a person.
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
	return roles
}
