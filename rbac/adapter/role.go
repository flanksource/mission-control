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

	// Resource are the Scopes the primary resource must belong to: the rule's, then the constraint's narrowed to the
	// types both select.
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

// UnappliedRule is an allow rule of a role that doesn't apply through a binding, because its constraint can't narrow it.
type UnappliedRule struct {
	Rule    string `json:"rule"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (u UnappliedRule) String() string {
	return fmt.Sprintf("rule %s: %s", u.Rule, u.Message)
}

// CompiledBinding is what a binding grants: the rules that apply through it, and the allow rules that don't.
type CompiledBinding struct {
	Rules     []CompiledRule
	Unapplied []UnappliedRule
}

// constraintScopes are the Scopes of a binding's constraint, resolved in the binding's namespace.
type constraintScopes struct {
	resource *ScopeSelection
	target   *ScopeSelection
}

// CompileBinding returns what a binding grants.
//
// A binding grants its whole role. Without a constraint, its rules apply as written. With one, every allow rule is
// narrowed by it, one side at a time, and an allow rule it can't narrow doesn't apply (see narrowRule).
// Deny rules are never narrowed: they apply whenever the role is valid, whatever happens to the constraint.
//
// The error says why the binding isn't Ready: its role is invalid, a Scope of its constraint is missing or invalid,
// or none of the role's allow rules applies through it. The rules returned still apply.
func CompileBinding(ctx context.Context, cache *gocache.Cache, binding models.RoleBinding, role models.Role) (CompiledBinding, error) {
	constraint, err := bindingConstraint(binding)
	if err != nil {
		return CompiledBinding{}, err
	}

	resolvedRules, err := ValidateRole(ctx, cache, role)
	if err != nil {
		return CompiledBinding{}, withContext(withReason(err, ReasonRoleInvalid), "role %s/%s is invalid", role.Namespace, role.Name)
	}

	var compiled CompiledBinding
	var allows []ResolvedRule
	for _, rule := range resolvedRules {
		if rule.Deny || constraint == nil {
			compiled.Rules = append(compiled.Rules, compileRule(binding, rule))
		} else {
			allows = append(allows, rule)
		}
	}

	if constraint == nil {
		return compiled, nil
	}

	scopes, err := resolveConstraintScopes(ctx, cache, binding.Namespace, *constraint)
	if err != nil {
		return compiled, withContext(err, "constraint")
	} else if len(allows) == 0 {
		return compiled, nil
	}

	for _, rule := range allows {
		c, err := narrowRule(ctx, compileRule(binding, rule), rule, scopes)
		if err != nil {
			if !IsValidationError(err) {
				return CompiledBinding{}, err
			}
			compiled.Unapplied = append(compiled.Unapplied, UnappliedRule{Rule: rule.Name, Reason: InvalidReason(err), Message: err.Error()})
			continue
		}

		compiled.Rules = append(compiled.Rules, c)
	}

	if len(compiled.Unapplied) == len(allows) {
		return compiled, NewInvalid(ReasonNoRulesApply, "none of the role's allow rules applies through the binding: %s",
			strings.Join(lo.Map(compiled.Unapplied, func(u UnappliedRule, _ int) string { return u.String() }), "; "))
	}

	return compiled, nil
}

// bindingConstraint returns the constraint of a stored binding, or nil when it has none.
func bindingConstraint(binding models.RoleBinding) (*v1.RoleBindingConstraint, error) {
	if binding.Constraint == nil {
		return nil, nil
	}

	var constraint *v1.RoleBindingConstraint
	if err := json.Unmarshal(*binding.Constraint, &constraint); err != nil {
		return nil, NewValidationError("role binding %s/%s: invalid constraint: %v", binding.Namespace, binding.Name, err)
	}
	return constraint, nil
}

// compileRule returns the rule as written, granted through the binding.
func compileRule(binding models.RoleBinding, rule ResolvedRule) CompiledRule {
	c := CompiledRule{
		ID:       BindingRuleID(binding.ID, rule.Name),
		Name:     rule.Name,
		Deny:     rule.Deny,
		Contract: rule.Contract,
		Resource: []ScopeSelection{rule.Resource},
	}
	if rule.Target != nil {
		c.Target = []ScopeSelection{*rule.Target}
	}
	return c
}

// resolveConstraintScopes resolves the Scopes a constraint names. A missing or invalid Scope is a validation error.
func resolveConstraintScopes(ctx context.Context, cache *gocache.Cache, namespace string, constraint v1.RoleBindingConstraint) (constraintScopes, error) {
	var scopes constraintScopes
	if constraint.Resource != nil {
		scope, err := ResolveScope(ctx, cache, namespace, constraint.Resource.ScopeRef)
		if err != nil {
			return constraintScopes{}, err
		}
		scopes.resource = &scope
	}

	if constraint.Target != nil {
		scope, err := ResolveScope(ctx, cache, namespace, constraint.Target.ScopeRef)
		if err != nil {
			return constraintScopes{}, err
		}
		scopes.target = &scope
	}

	return scopes, nil
}

// narrowRule narrows an allow rule by the constraint's Scopes, one side at a time.
// A side the rule's action can't take is skipped. A validation error means the rule doesn't apply through the binding:
//
//   - the action takes a target, the constraint sets one, and the rule has none: the rule allows only operations
//     without a target, so its set of targets is empty and stays empty when narrowed
//   - no side narrows the rule, so leaving it would grant it as written
//   - an input can't be narrowed (see narrowInput)
func narrowRule(ctx context.Context, compiled CompiledRule, rule ResolvedRule, scopes constraintScopes) (CompiledRule, error) {
	narrowed := false
	if scopes.resource != nil {
		scope, err := narrowInput(ctx, rule.Contract, "resource", rule.Resource, *scopes.resource)
		if err != nil {
			return CompiledRule{}, err
		}
		compiled.Resource = append(compiled.Resource, scope)
		narrowed = true
	}

	if scopes.target != nil && len(rule.Contract.Targets) > 0 {
		if rule.Target == nil {
			return CompiledRule{}, NewInvalid(ReasonConstraintDoesNotFit,
				"%s takes a target and the rule has none, so it only allows operations without a target, which the constraint's target can't narrow", rule.Action)
		}

		scope, err := narrowInput(ctx, rule.Contract, "target", *rule.Target, *scopes.target)
		if err != nil {
			return CompiledRule{}, err
		}
		compiled.Target = append(compiled.Target, scope)
		narrowed = true
	}

	if !narrowed {
		return CompiledRule{}, NewInvalid(ReasonConstraintDoesNotFit,
			"%s takes no target and the constraint sets no resource, so the constraint can't narrow the rule", rule.Action)
	}

	return compiled, nil
}

// narrowInput returns the part of the constraint's Scope that narrows an input of the rule: its targets of the types
// both Scopes select. Types the input can't carry are ignored, so one Scope can narrow every input of every rule.
//
// The input can't be narrowed when the Scopes share no type, or when those targets can't be enforced for the action
// as the rule's own Scope must be: whole-type targets for create, update and delete, and row filters for read,
// which need row-level security unless every target is a whole-type target.
func narrowInput(ctx context.Context, contract ActionContract, input string, ruleScope, constraintScope ScopeSelection) (ScopeSelection, error) {
	common := lo.Intersect(ruleScope.DeclaredTypes(), constraintScope.DeclaredTypes())
	if len(common) == 0 {
		return ScopeSelection{}, NewInvalid(ReasonConstraintDoesNotFit, "the rule's %s scope %s selects %s and the constraint's scope %s selects %s: they share no type",
			input, ruleScope.Name, strings.Join(ruleScope.DeclaredTypes(), ", "), constraintScope.Name, strings.Join(constraintScope.DeclaredTypes(), ", "))
	}

	narrowed := ScopeSelection{ID: constraintScope.ID, Name: constraintScope.Name, Selectors: map[string][]types.ResourceSelector{}}
	for _, kind := range common {
		for _, selector := range constraintScope.Selectors[kind] {
			if err := contract.enforceable(kind, selector); err != nil {
				return ScopeSelection{}, NewInvalid(ReasonConstraintDoesNotFit, "constraint scope %s: %v", constraintScope.Name, err)
			}
		}
		narrowed.Selectors[kind] = constraintScope.Selectors[kind]
	}

	if contract.requiresRowLevelSecurity(narrowed) && !rowLevelSecurityEnabled(ctx) {
		return ScopeSelection{}, NewInvalid(ReasonRowLevelSecurityRequired,
			"constraint scope %s doesn't select whole types, so %s needs row-level security (%s) to filter listings", constraintScope.Name, contract.Action, vars.FlagRLSEnable)
	}

	return narrowed, nil
}

// ValidateBinding validates a stored binding against its role and the Scopes its constraint names,
// and returns what it grants.
func ValidateBinding(ctx context.Context, cache *gocache.Cache, binding models.RoleBinding) (CompiledBinding, error) {
	if _, err := RoleBindingSpec(binding); err != nil {
		return CompiledBinding{}, err
	}

	var role models.Role
	if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", binding.Namespace, binding.Role).Find(&role).Error; err != nil {
		return CompiledBinding{}, err
	} else if role.ID == uuid.Nil {
		return CompiledBinding{}, NewInvalid(ReasonRoleNotFound, "role %s/%s not found", binding.Namespace, binding.Role)
	}

	return CompileBinding(ctx, cache, binding, role)
}

// BindingsWithUnappliedRules returns the bindings of the role that some of its allow rules don't apply through.
func BindingsWithUnappliedRules(ctx context.Context, role models.Role) ([]v1.BindingUnappliedRules, error) {
	var bindings []models.RoleBinding
	if err := ctx.DB().Where("namespace = ? AND role = ? AND deleted_at IS NULL", role.Namespace, role.Name).
		Order("name").Find(&bindings).Error; err != nil {
		return nil, err
	}

	var result []v1.BindingUnappliedRules
	for _, binding := range bindings {
		compiled, err := CompileBinding(ctx, nil, binding, role)
		if err != nil && !IsValidationError(err) {
			return nil, err
		} else if len(compiled.Unapplied) == 0 {
			continue
		}

		result = append(result, v1.BindingUnappliedRules{
			Name:  binding.Name,
			Rules: lo.Map(compiled.Unapplied, func(u UnappliedRule, _ int) string { return u.Rule }),
		})
	}

	return result, nil
}

// LoadBindingRules returns the rules that apply through the binding with the given id.
// A binding that doesn't exist, or whose role doesn't exist or is invalid, grants nothing.
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

	compiled, err := CompileBinding(ctx, nil, binding, role)
	if err != nil && !IsValidationError(err) {
		return nil, err
	}
	return compiled.Rules, nil
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
