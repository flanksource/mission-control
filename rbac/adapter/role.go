package adapter

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/vars"
)

// BindingRuleID identifies a rule of a role as granted by a binding. It's carried in the last field of the
// casbin policies compiled from the rule, so a policy can be traced back to the binding and rule that produced it.
func BindingRuleID(bindingID uuid.UUID, rule string) string {
	return fmt.Sprintf("%s%s/%s", models.BindingPrincipalPrefix, bindingID, rule)
}

// ParseBindingRuleID is the inverse of BindingRuleID.
func ParseBindingRuleID(id string) (bindingID uuid.UUID, rule string, ok bool) {
	ref, found := strings.CutPrefix(id, models.BindingPrincipalPrefix)
	if !found {
		return uuid.Nil, "", false
	}

	rawID, rule, found := strings.Cut(ref, "/")
	if !found || rule == "" {
		return uuid.Nil, "", false
	}

	bindingID, err := uuid.Parse(rawID)
	if err != nil {
		return uuid.Nil, "", false
	}

	return bindingID, rule, true
}

// ResolvedRule is a validated rule of a role, with its Scopes resolved.
type ResolvedRule struct {
	v1.RoleRule
	Contract ActionContract
	Resource ScopeSelection
	Target   *ScopeSelection
}

// reservedRoleNames are the built-in roles. A Role can't be named after one, so a binding's role never reads as a built-in role.
var reservedRoleNames = []string{
	policy.RoleAdmin, policy.RoleEveryone, policy.RoleGuest, policy.RoleViewer,
	policy.RoleEditor, policy.RoleCommander, policy.RoleResponder, policy.RoleAgent,
}

// ValidateRoleName checks that a Role isn't named after a built-in role.
func ValidateRoleName(name string) error {
	if slices.Contains(reservedRoleNames, name) {
		return NewValidationError("a role can't be named after the built-in role %q", name)
	}
	return nil
}

// rowLevelSecurityEnabled reports whether listings are filtered by row.
func rowLevelSecurityEnabled(ctx context.Context) bool {
	return vars.RLSEnabled(ctx)
}

// ResolveRoleRules validates the rules of a role and resolves their Scopes in the role's namespace.
// A role is valid as a whole or not at all: every problem is a validation error.
func ResolveRoleRules(ctx context.Context, cache *gocache.Cache, namespace string, rules []v1.RoleRule) ([]ResolvedRule, error) {
	if err := (v1.RoleSpec{Rules: rules}).Validate(); err != nil {
		return nil, NewValidationError("%v", err)
	}

	resolved := make([]ResolvedRule, 0, len(rules))
	for _, rule := range rules {
		r, err := resolveRule(ctx, cache, namespace, rule)
		if err != nil {
			return nil, withContext(err, "rule %s", rule.Name)
		}
		resolved = append(resolved, r)
	}

	return resolved, nil
}

// ValidateRoleSpec checks a role on its own, without the Scopes and plugins its rules reference.
// A role that fails it can never become valid, so the API rejects it.
func ValidateRoleSpec(name string, rules []v1.RoleRule) error {
	if err := ValidateRoleName(name); err != nil {
		return err
	}

	if err := (v1.RoleSpec{Rules: rules}).Validate(); err != nil {
		return NewValidationError("%v", err)
	}

	for _, rule := range rules {
		if _, err := ruleContract(rule); err != nil {
			return withContext(err, "rule %s", rule.Name)
		}
	}

	return nil
}

// ruleContract returns the contract of the rule's action, and checks the rule fits it on its own:
// a deny the action can enforce, and a target only on an action that takes one.
func ruleContract(rule v1.RoleRule) (ActionContract, error) {
	contract, err := ContractFor(rule.Action)
	if err != nil {
		return ActionContract{}, NewValidationError("%v", err)
	}

	if rule.Deny && !contract.AllowsDeny {
		return ActionContract{}, NewValidationError("%s can't be denied yet: built-in read access, and listings not being filtered by row for Mission Control's own users, would bypass the deny", rule.Action)
	}

	if rule.Target != nil && len(contract.Targets) == 0 {
		return ActionContract{}, NewValidationError("%s doesn't take a target", rule.Action)
	}

	return contract, nil
}

func resolveRule(ctx context.Context, cache *gocache.Cache, namespace string, rule v1.RoleRule) (ResolvedRule, error) {
	contract, err := ruleContract(rule)
	if err != nil {
		return ResolvedRule{}, err
	}

	resource, err := resolveInput(ctx, cache, namespace, contract, "resource", rule.Resource.ScopeRef)
	if err != nil {
		return ResolvedRule{}, err
	}

	resolved := ResolvedRule{RoleRule: rule, Contract: contract, Resource: resource}
	if rule.Target == nil {
		return resolved, nil
	}

	target, err := resolveInput(ctx, cache, namespace, contract, "target", rule.Target.ScopeRef)
	if err != nil {
		return ResolvedRule{}, err
	}

	resolved.Target = &target
	return resolved, nil
}

// resolveInput resolves the Scope filling an input of the action, and checks the action can use it.
func resolveInput(ctx context.Context, cache *gocache.Cache, namespace string, contract ActionContract, input, scopeRef string) (ScopeSelection, error) {
	scope, err := ResolveScope(ctx, cache, namespace, scopeRef)
	if err != nil {
		return ScopeSelection{}, err
	} else if err := contract.validateInput(input, scope); err != nil {
		return ScopeSelection{}, NewValidationError("%v", err)
	}

	if contract.requiresRowLevelSecurity(scope) && !rowLevelSecurityEnabled(ctx) {
		return ScopeSelection{}, NewInvalid(ReasonRowLevelSecurityRequired,
			"scope %s doesn't select whole types, so %s needs row-level security (%s) to filter listings", scope.Name, contract.Action, vars.FlagRLSEnable)
	}

	return scope, nil
}

// resolvedConstraint is a validated constraint of a binding, with its Scopes resolved.
type resolvedConstraint struct {
	resource *ScopeSelection
	target   *ScopeSelection
}

// ResolveConstraints validates the constraints of a binding against the rules of its role,
// and resolves their Scopes in the binding's namespace.
//
// A constraint can only narrow an allow rule: it can't name a deny rule, since narrowing a deny loosens the role,
// and it can't give a target to a rule without one, since that would widen the rule.
// Its Scopes must meet everything the rule's own Scopes must meet for the action.
func ResolveConstraints(ctx context.Context, cache *gocache.Cache, namespace string, constraints []v1.RoleBindingConstraint, rules []ResolvedRule) (map[string]resolvedConstraint, error) {
	resolved := map[string]resolvedConstraint{}
	for _, constraint := range constraints {
		c, err := resolveConstraint(ctx, cache, namespace, constraint, rules, resolved)
		if err != nil {
			return nil, withContext(err, "constraint on rule %s", constraint.Rule)
		}
		resolved[constraint.Rule] = c
	}

	return resolved, nil
}

func resolveConstraint(ctx context.Context, cache *gocache.Cache, namespace string, constraint v1.RoleBindingConstraint, rules []ResolvedRule, resolved map[string]resolvedConstraint) (resolvedConstraint, error) {
	if _, ok := resolved[constraint.Rule]; ok {
		return resolvedConstraint{}, NewValidationError("the rule is constrained more than once")
	}

	rule, found := lo.Find(rules, func(r ResolvedRule) bool { return r.Name == constraint.Rule })
	if !found {
		return resolvedConstraint{}, NewValidationError("the role has no such rule")
	} else if rule.Deny {
		return resolvedConstraint{}, NewValidationError("deny rules can't be constrained: narrowing a deny loosens the role")
	}

	var c resolvedConstraint
	if constraint.Resource != nil {
		scope, err := resolveConstraintInput(ctx, cache, namespace, rule, "resource", rule.Resource, constraint.Resource.ScopeRef)
		if err != nil {
			return resolvedConstraint{}, err
		}
		c.resource = &scope
	}

	if constraint.Target != nil {
		if rule.Target == nil {
			return resolvedConstraint{}, NewValidationError("the rule has no target, and a constraint can't add one")
		}

		scope, err := resolveConstraintInput(ctx, cache, namespace, rule, "target", *rule.Target, constraint.Target.ScopeRef)
		if err != nil {
			return resolvedConstraint{}, err
		}
		c.target = &scope
	}

	return c, nil
}

// resolveConstraintInput resolves the Scope a constraint narrows an input of the rule with.
// At least one type it selects must also be selected by the rule's Scope, or nothing would be in both.
func resolveConstraintInput(ctx context.Context, cache *gocache.Cache, namespace string, rule ResolvedRule, input string, ruleScope ScopeSelection, scopeRef string) (ScopeSelection, error) {
	scope, err := resolveInput(ctx, cache, namespace, rule.Contract, input, scopeRef)
	if err != nil {
		return ScopeSelection{}, err
	}

	if !lo.Some(scope.DeclaredTypes(), ruleScope.DeclaredTypes()) {
		return ScopeSelection{}, NewValidationError("scope %s selects %s and the rule's %s scope %s selects %s: no resource can be in both",
			scope.Name, strings.Join(scope.DeclaredTypes(), ", "), input, ruleScope.Name, strings.Join(ruleScope.DeclaredTypes(), ", "))
	}

	return scope, nil
}

// withContext prefixes a validation error, keeping its reason. Other errors are returned as they are.
func withContext(err error, format string, args ...any) error {
	if IsValidationError(err) {
		return NewInvalid(InvalidReason(err), "%s: %v", fmt.Sprintf(format, args...), err)
	}
	return err
}

// CompiledRule is a rule of a role as granted through a binding.
type CompiledRule struct {
	ID       string
	Name     string
	Deny     bool
	Contract ActionContract

	// Resource are the Scopes the primary resource must belong to: the rule's, then the constraint's.
	Resource []ScopeSelection

	// Target are the Scopes the target must belong to. Empty when the rule has no target.
	Target []ScopeSelection
}

// ValidateRole validates a stored role against the Scopes it references, and returns its resolved rules.
func ValidateRole(ctx context.Context, cache *gocache.Cache, role models.Role) ([]ResolvedRule, error) {
	if err := ValidateRoleName(role.Name); err != nil {
		return nil, err
	}

	var rules []v1.RoleRule
	if err := json.Unmarshal(role.Rules, &rules); err != nil {
		return nil, NewValidationError("invalid rules: %v", err)
	}

	return ResolveRoleRules(ctx, cache, role.Namespace, rules)
}

// ValidateStoredScope validates a stored Scope on its own.
func ValidateStoredScope(ctx context.Context, cache *gocache.Cache, scope models.Scope) error {
	var targets []v1.ScopeTarget
	if err := json.Unmarshal(scope.Targets, &targets); err != nil {
		return NewValidationError("invalid targets: %v", err)
	}

	_, err := ValidateScope(ctx, cache, targets)
	return err
}

// CompileBinding returns the rules a binding grants.
//
// Without constraints, those are all the rules of its role. With constraints, they're the role's deny rules,
// as written, and the allow rules the constraints name, narrowed by them.
// A binding of an invalid role, or with an invalid constraint, grants nothing.
func CompileBinding(ctx context.Context, cache *gocache.Cache, binding models.RoleBinding, role models.Role) ([]CompiledRule, error) {
	var constraints []v1.RoleBindingConstraint
	if len(binding.Constraints) > 0 {
		if err := json.Unmarshal(binding.Constraints, &constraints); err != nil {
			return nil, NewValidationError("role binding %s/%s: invalid constraints: %v", binding.Namespace, binding.Name, err)
		}
	}

	resolvedRules, err := ValidateRole(ctx, cache, role)
	if err != nil {
		return nil, withContext(withReason(err, ReasonRoleInvalid), "role %s/%s is invalid", role.Namespace, role.Name)
	}

	resolvedConstraints, err := ResolveConstraints(ctx, cache, binding.Namespace, constraints, resolvedRules)
	if err != nil {
		return nil, err
	}

	var compiled []CompiledRule
	for _, rule := range resolvedRules {
		constraint, constrained := resolvedConstraints[rule.Name]
		if len(constraints) > 0 && !rule.Deny && !constrained {
			continue
		}

		c := CompiledRule{
			ID:       BindingRuleID(binding.ID, rule.Name),
			Name:     rule.Name,
			Deny:     rule.Deny,
			Contract: rule.Contract,
			Resource: []ScopeSelection{rule.Resource},
		}

		if constraint.resource != nil {
			c.Resource = append(c.Resource, *constraint.resource)
		}

		if rule.Target != nil {
			c.Target = []ScopeSelection{*rule.Target}
			if constraint.target != nil {
				c.Target = append(c.Target, *constraint.target)
			}
		}

		compiled = append(compiled, c)
	}

	return compiled, nil
}

// ValidateBinding validates a stored binding against its role and the Scopes its constraints name.
func ValidateBinding(ctx context.Context, cache *gocache.Cache, binding models.RoleBinding) error {
	if _, err := RoleBindingSpec(binding); err != nil {
		return err
	}

	var role models.Role
	if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", binding.Namespace, binding.Role).Find(&role).Error; err != nil {
		return err
	} else if role.ID == uuid.Nil {
		return NewInvalid(ReasonRoleNotFound, "role %s/%s not found", binding.Namespace, binding.Role)
	}

	_, err := CompileBinding(ctx, cache, binding, role)
	return err
}

// LoadBindingRules returns the rules granted by the binding with the given id.
// A binding that doesn't exist, or whose role doesn't exist, grants nothing.
func LoadBindingRules(ctx context.Context, bindingID uuid.UUID) ([]CompiledRule, error) {
	var binding models.RoleBinding
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", bindingID).Find(&binding).Error; err != nil {
		return nil, err
	} else if binding.ID == uuid.Nil {
		return nil, nil
	}

	if _, err := RoleBindingSpec(binding); err != nil {
		return nil, err
	}

	var role models.Role
	if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", binding.Namespace, binding.Role).Find(&role).Error; err != nil {
		return nil, err
	} else if role.ID == uuid.Nil {
		return nil, nil
	}

	return CompileBinding(ctx, nil, binding, role)
}

// RowFilters returns the row filters of a read rule, by resource type: the rows that belong to every Scope of the rule.
func (r CompiledRule) RowFilters() (map[string][]rls.Scope, error) {
	filters := map[string][]rls.Scope{}
	for _, kind := range rowFilterKinds {
		var current []rls.Scope
		for i, scope := range r.Resource {
			var scopeFilters []rls.Scope
			for _, selector := range scope.Selectors[kind] {
				filter, err := RowFilter(kind, selector)
				if err != nil {
					return nil, fmt.Errorf("rule %s: %w", r.Name, err)
				} else if filter != nil {
					scopeFilters = append(scopeFilters, *filter)
				}
			}

			if i == 0 {
				current = scopeFilters
				continue
			}

			var narrowed []rls.Scope
			for _, a := range current {
				for _, b := range scopeFilters {
					if filter, ok := IntersectRowFilters(a, b); ok {
						narrowed = append(narrowed, filter)
					}
				}
			}
			current = narrowed
		}

		if len(current) > 0 {
			filters[kind] = current
		}
	}

	return filters, nil
}

// condition returns the casbin condition that matches the rule against a request.
func (r CompiledRule) condition() (string, error) {
	condition := dutyRBAC.RuleCondition{
		ResourceTypes: r.Contract.Resources,
		TargetTypes:   r.Contract.Targets,
		Deny:          r.Deny,
	}
	for _, scope := range r.Resource {
		condition.Resource = append(condition.Resource, scope.Selectors)
	}
	for _, scope := range r.Target {
		condition.Target = append(condition.Target, scope.Selectors)
	}

	raw, err := json.Marshal(condition)
	if err != nil {
		return "", fmt.Errorf("failed to marshal the condition of rule %s: %w", r.Name, err)
	}
	return fmt.Sprintf(`matchRule(r.obj, %q)`, string(raw)), nil
}

// wholeTypeObjects returns the RBAC objects of the resource types that every Scope of the rule's resource
// selects entirely, e.g. catalog when every Scope has config: {name: "*"}.
func (r CompiledRule) wholeTypeObjects() []string {
	var objects []string
	for _, kind := range r.Resource[0].DeclaredTypes() {
		object, ok := objectForType[kind]
		if !ok {
			continue
		}

		whole := lo.EveryBy(r.Resource, func(scope ScopeSelection) bool {
			return lo.SomeBy(scope.Selectors[kind], func(s types.ResourceSelector) bool { return s.Wildcard() })
		})
		if whole {
			objects = append(objects, object)
		}
	}

	return objects
}

// compiledRuleToCasbinRules compiles a rule granted by a binding into casbin policies filed under the binding's principal.
//
// A rule checked on the resources of each request is matched by matchRule:
//
//	p, binding:<ns>/<name>, *, <action>, <allow|deny>, matchRule(r.obj, <condition>), binding:<binding id>/<rule>
//
// A rule checked on whole object types is granted on the object of each type it selects entirely, e.g. catalog.
// So is a read rule, for endpoints that check reads on whole object types.
func compiledRuleToCasbinRules(principal string, rule CompiledRule) ([][]string, error) {
	effect := "allow"
	if rule.Deny {
		effect = "deny"
	}

	var policies [][]string
	if rule.Contract.enforcement != enforcedOnObjects {
		condition, err := rule.condition()
		if err != nil {
			return nil, err
		}
		policies = append(policies, []string{"p", principal, "*", rule.Contract.Action, effect, condition, rule.ID})
	}

	if rule.Contract.enforcement == enforcedOnObjects || rule.Contract.enforcement == enforcedWithRows {
		for _, object := range rule.wholeTypeObjects() {
			policies = append(policies, []string{"p", principal, object, rule.Contract.Action, effect, "", rule.ID})
		}
	}

	return policies, nil
}
