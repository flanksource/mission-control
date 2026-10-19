package adapter

import (
	"crypto/md5"
	"encoding/hex"
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

// The RoleBindings created with an access token, one per Role it's created with, have this source and are in this
// reserved namespace (specs/authorization/token-bindings.md). Nothing else can be stored there.
const (
	SourceAccessToken    = "AccessToken"
	AccessTokenNamespace = "mission-control-access-tokens"
)

// AccessTokenBindingName returns the name of the RoleBinding created with an access token for one of its Roles.
// The token id and the hash are fixed length, so the name fits a Kubernetes name.
func AccessTokenBindingName(tokenID uuid.UUID, roleNamespace, roleName string) string {
	sum := md5.Sum([]byte(roleNamespace + "/" + roleName))
	return fmt.Sprintf("token-%s-%s", tokenID, hex.EncodeToString(sum[:])[:12])
}

// accessTokenSubject is the subject a RoleBinding created with an access token stores, and its only one.
// It's not part of v1.RoleBindingSubjects: neither a custom resource nor the RoleBinding API can select a token.
type accessTokenSubject struct {
	AccessToken string `json:"accessToken"`
}

// AccessTokenBindingSubjects returns the stored subjects of a RoleBinding that selects the access token.
func AccessTokenBindingSubjects(tokenID uuid.UUID) ([]byte, error) {
	return json.Marshal(accessTokenSubject{AccessToken: tokenID.String()})
}

// bindingAccessToken returns the access token a stored binding selects, if it's a binding created with a token.
// Such a binding has no other subject and no constraint, and is in the reserved namespace.
func bindingAccessToken(binding models.RoleBinding) (uuid.UUID, bool, error) {
	if len(binding.Subjects) == 0 {
		return uuid.Nil, false, nil
	}

	var subjects map[string]json.RawMessage
	if err := json.Unmarshal(binding.Subjects, &subjects); err != nil {
		return uuid.Nil, false, NewValidationError("role binding %s/%s: invalid subjects: %v", binding.Namespace, binding.Name, err)
	}

	raw, ok := subjects["accessToken"]
	if !ok {
		if binding.Namespace == AccessTokenNamespace {
			return uuid.Nil, false, NewValidationError("role binding %s/%s: namespace %s holds only access tokens' bindings", binding.Namespace, binding.Name, AccessTokenNamespace)
		}
		return uuid.Nil, false, nil
	}

	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return uuid.Nil, false, NewValidationError("role binding %s/%s: invalid access token subject: %v", binding.Namespace, binding.Name, err)
	}
	tokenID, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, false, NewValidationError("role binding %s/%s: %q isn't an access token id", binding.Namespace, binding.Name, id)
	}

	switch {
	case len(subjects) > 1:
		return uuid.Nil, false, NewValidationError("role binding %s/%s: a binding that selects an access token can't select any other subject", binding.Namespace, binding.Name)
	case binding.Namespace != AccessTokenNamespace || binding.Source != SourceAccessToken:
		return uuid.Nil, false, NewValidationError("role binding %s/%s: only an access token's own bindings, in %s, can select it", binding.Namespace, binding.Name, AccessTokenNamespace)
	case binding.Constraint != nil:
		return uuid.Nil, false, NewValidationError("role binding %s/%s: an access token's binding has no constraint", binding.Namespace, binding.Name)
	case binding.Deny:
		return uuid.Nil, false, NewValidationError("role binding %s/%s: an access token's binding can't deny", binding.Namespace, binding.Name)
	}

	return tokenID, true, nil
}

// AccessTokenBindings returns the RoleBindings that select the access token.
func AccessTokenBindings(ctx context.Context, tokenID uuid.UUID) ([]models.RoleBinding, error) {
	var bindings []models.RoleBinding
	err := ctx.DB().Where("deleted_at IS NULL AND namespace = ? AND source = ?", AccessTokenNamespace, SourceAccessToken).
		Where("subjects->>'accessToken' = ?", tokenID.String()).
		Order("role").
		Find(&bindings).Error
	return bindings, err
}

// LoadAccessTokenLimitRules returns the rules that limit the access token: those of every valid RoleBinding that
// selects it, narrowed by its constraint. A token no binding selects has none, so it matches nothing.
func LoadAccessTokenLimitRules(ctx context.Context, tokenID uuid.UUID) ([]CompiledRule, error) {
	bindings, err := AccessTokenBindings(ctx, tokenID)
	if err != nil {
		return nil, err
	}

	var rules []CompiledRule
	for _, binding := range bindings {
		if binding.Error != nil {
			continue
		}

		namespace, name, err := BindingRoleRef(binding)
		if err != nil {
			continue
		}

		var role models.Role
		if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL AND error IS NULL", namespace, name).Find(&role).Error; err != nil {
			return nil, err
		} else if role.ID == uuid.Nil {
			continue
		}

		compiled, err := CompileBinding(ctx, nil, binding, role)
		if err != nil && !IsValidationError(err) {
			return nil, err
		}
		rules = append(rules, compiled.Rules...)
	}
	return rules, nil
}

// ResolvedRule is a validated rule of a role, with its Scopes resolved.
type ResolvedRule struct {
	v1.RoleRule
	Contract ActionContract

	// Resource is nil when the action takes no resource.
	Resource *ScopeSelection
	Target   *ScopeSelection
}

// Shipped Roles and Scopes are written by Mission Control itself, in this namespace and with this source
// (see specs/authorization/overview.md, "Shipped Roles").
const (
	ShippedNamespace = "mission-control-builtin"
	SourceShipped    = "mission-control"
)

// IsShipped reports whether an object is one Mission Control ships.
func IsShipped(namespace, source string) bool {
	return namespace == ShippedNamespace && source == SourceShipped
}

// RolePluginAdmin is the shipped Role that allows every operation of the installed plugins. It's only written while
// some plugin has an operation.
const RolePluginAdmin = "plugin-admin"

// BindingPluginAdmins is the shipped RoleBinding of the admins team to plugin-admin, written only while plugin-admin is.
const BindingPluginAdmins = "plugin-admins"

// ResolveRoleRules validates the rules of a role and resolves their Scopes from the role's namespace.
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
	if err := (v1.RoleSpec{Rules: rules}).Validate(); err != nil {
		return NewValidationError("%v", err)
	} else if len(rules) > v1.MaxRoleRules {
		return NewValidationError("a role can have at most %d rules, not %d", v1.MaxRoleRules, len(rules))
	}

	for _, rule := range rules {
		if _, err := ruleContract(rule); err != nil {
			return withContext(err, "rule %s", rule.Name)
		}
	}

	return nil
}

// ruleContract returns the contract of the rule's action, and checks the rule fits it on its own:
// no deny, a resource exactly when the action takes one, and a target only on an action that takes one.
func ruleContract(rule v1.RoleRule) (ActionContract, error) {
	if rule.Deny {
		return ActionContract{}, NewValidationError("deny isn't supported on a rule: rules only allow, and a RoleBinding with deny refuses a role")
	}

	contract, err := ContractFor(rule.Action)
	if err != nil {
		return ActionContract{}, NewValidationError("%v", err)
	}

	if contract.TakesResource() && rule.Resource == nil {
		return ActionContract{}, NewValidationError("%s needs a resource", rule.Action)
	} else if !contract.TakesResource() && rule.Resource != nil {
		return ActionContract{}, NewValidationError("%s takes no resource", rule.Action)
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

	resolved := ResolvedRule{RoleRule: rule, Contract: contract}
	if rule.Resource == nil {
		return resolved, nil
	}

	resource, err := resolveInput(ctx, cache, namespace, contract, "resource", rule.Resource.ScopeRef)
	if err != nil {
		return ResolvedRule{}, err
	}

	resolved.Resource = &resource
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

	return scope, nil
}

// withContext prefixes a validation error, keeping its reason. Other errors are returned as they are.
func withContext(err error, format string, args ...any) error {
	if IsValidationError(err) {
		return NewInvalid(InvalidReason(err), "%s: %v", fmt.Sprintf(format, args...), err)
	}
	return err
}

// CompiledRule is a rule of a role as granted, or denied, through a binding.
type CompiledRule struct {
	ID       string
	Name     string
	Contract ActionContract

	// Deny is set for a rule of a deny binding: it refuses what it matches instead of granting it.
	Deny bool

	// Resource are the Scopes the primary resource must belong to: the rule's, then the constraint's narrowed to the
	// types both select. Empty when the action takes no resource.
	Resource []ScopeSelection

	// Target are the Scopes the target must belong to. Empty when the rule has no target.
	Target []ScopeSelection
}

// ValidateRole validates a stored role against the Scopes it references, and returns its resolved rules.
func ValidateRole(ctx context.Context, cache *gocache.Cache, role models.Role) ([]ResolvedRule, error) {
	var rules []v1.RoleRule
	if err := json.Unmarshal(role.Rules, &rules); err != nil {
		return nil, NewValidationError("invalid rules: %v", err)
	}

	return ResolveRoleRules(ctx, cache, role.Namespace, rules)
}

// UnappliedRule is a rule of a role that doesn't apply through a binding: its constraint can't narrow it, or it's
// a deny of RoleBindings alone.
type UnappliedRule struct {
	Rule    string `json:"rule"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (u UnappliedRule) String() string {
	return fmt.Sprintf("rule %s: %s", u.Rule, u.Message)
}

// CompiledBinding is what a binding grants: the rules that apply through it, and the rules that don't.
type CompiledBinding struct {
	Rules     []CompiledRule
	Unapplied []UnappliedRule
}

// constraintScopes are the Scopes of a binding's constraint, resolved from the binding's namespace.
type constraintScopes struct {
	resource *ScopeSelection
	target   *ScopeSelection
}

// CompileBinding returns what a binding grants, or with deny, refuses.
//
// A binding grants its whole role. Without a constraint, its rules apply as written. With one, every rule is
// narrowed by it, one side at a time, and a rule it can't narrow doesn't apply (see narrowRule).
// A deny binding's rules never select RoleBindings (see withoutRoleBindings).
//
// The error says why the binding isn't Ready: its role is invalid, a Scope of its constraint is missing or invalid,
// or none of the role's rules applies through it. The rules returned still apply.
func CompileBinding(ctx context.Context, cache *gocache.Cache, binding models.RoleBinding, role models.Role) (CompiledBinding, error) {
	constraint, err := bindingConstraint(binding)
	if err != nil {
		return CompiledBinding{}, err
	}

	resolvedRules, err := ValidateRole(ctx, cache, role)
	if err != nil {
		return CompiledBinding{}, withContext(withReason(err, ReasonRoleInvalid), "role %s/%s is invalid", role.Namespace, role.Name)
	}

	if selectsPlugins, err := bindingSelectsPlugins(binding); err != nil {
		return CompiledBinding{}, err
	} else if selectsPlugins {
		if err := checkPluginRules(resolvedRules); err != nil {
			return CompiledBinding{}, withContext(err, "role %s/%s can't be granted to a plugin", role.Namespace, role.Name)
		}
	}

	var compiled CompiledBinding
	unapplied := func(rule ResolvedRule, err error) {
		compiled.Unapplied = append(compiled.Unapplied, UnappliedRule{Rule: rule.Name, Reason: InvalidReason(err), Message: err.Error()})
	}

	if binding.Deny {
		denied := make([]ResolvedRule, 0, len(resolvedRules))
		for _, rule := range resolvedRules {
			if rule, err := withoutRoleBindings(rule); err != nil {
				unapplied(rule, err)
			} else {
				denied = append(denied, rule)
			}
		}
		resolvedRules = denied
	}

	var scopes constraintScopes
	if constraint != nil {
		if scopes, err = resolveConstraintScopes(ctx, cache, binding.Namespace, *constraint); err != nil {
			return compiled, withContext(err, "constraint")
		}
	}

	for _, rule := range resolvedRules {
		if constraint == nil {
			compiled.Rules = append(compiled.Rules, compileRule(binding, rule))
			continue
		}

		c, err := narrowRule(ctx, compileRule(binding, rule), rule, scopes)
		if err != nil {
			if !IsValidationError(err) {
				return CompiledBinding{}, err
			}
			unapplied(rule, err)
			continue
		}

		compiled.Rules = append(compiled.Rules, c)
	}

	if len(compiled.Rules) == 0 {
		return compiled, NewInvalid(ReasonNoRulesApply, "none of the role's rules applies through the binding: %s",
			strings.Join(lo.Map(compiled.Unapplied, func(u UnappliedRule, _ int) string { return u.String() }), "; "))
	}

	return compiled, nil
}

// withoutRoleBindings returns a rule of a deny binding with RoleBindings left out of its Scopes, so that whoever may
// manage RoleBindings can always remove a deny binding. A rule that only selects RoleBindings doesn't apply.
func withoutRoleBindings(rule ResolvedRule) (ResolvedRule, error) {
	if rule.Resource == nil || !lo.Contains(rule.Resource.DeclaredTypes(), v1.ResourceRoleBinding) {
		return rule, nil
	}

	if len(rule.Resource.Selectors) == 1 {
		return rule, NewInvalid(ReasonDenyOnRoleBindings,
			"scope %s only selects RoleBindings, which a deny binding never denies", rule.Resource.Name)
	}

	resource := *rule.Resource
	resource.Selectors = lo.OmitByKeys(resource.Selectors, []string{v1.ResourceRoleBinding})
	rule.Resource = &resource
	return rule, nil
}

// pluginActions are the actions a plugin can be granted, on connections only.
var pluginActions = []string{policy.ActionRead, ActionConnectionUse}

// bindingSelectsPlugins reports whether a stored binding has plugin subjects.
func bindingSelectsPlugins(binding models.RoleBinding) (bool, error) {
	if len(binding.Subjects) == 0 {
		return false, nil
	}

	var subjects v1.RoleBindingSubjects
	if err := json.Unmarshal(binding.Subjects, &subjects); err != nil {
		return false, NewValidationError("role binding %s/%s: invalid subjects: %v", binding.Namespace, binding.Name, err)
	}
	return len(subjects.Plugins) > 0, nil
}

// checkPluginRules checks that every rule of a role is one a plugin can be granted: one of pluginActions,
// over Scopes that select connections and nothing else.
func checkPluginRules(rules []ResolvedRule) error {
	for _, rule := range rules {
		if !slices.Contains(pluginActions, rule.Action) {
			return NewInvalid(ReasonPluginRuleNotAllowed, "rule %s: a plugin can only be granted %s, not %s",
				rule.Name, strings.Join(pluginActions, ", "), rule.Action)
		}

		for _, scope := range []*ScopeSelection{rule.Resource, rule.Target} {
			if scope == nil {
				continue
			}

			if kinds := scope.DeclaredTypes(); !slices.Equal(kinds, []string{policy.ResourceConnection}) {
				return NewInvalid(ReasonPluginRuleNotAllowed, "rule %s: a plugin can only be granted rules on connections, and scope %s selects %s",
					rule.Name, scope.Name, strings.Join(kinds, ", "))
			}
		}
	}

	return nil
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

// compileRule returns the rule as written, granted, or denied, through the binding.
func compileRule(binding models.RoleBinding, rule ResolvedRule) CompiledRule {
	compiled := compileResolvedRule(BindingRuleID(binding.ID, rule.Name), rule)
	compiled.Deny = binding.Deny
	return compiled
}

func compileResolvedRule(id string, rule ResolvedRule) CompiledRule {
	c := CompiledRule{
		ID:       id,
		Name:     rule.Name,
		Contract: rule.Contract,
	}
	if rule.Resource != nil {
		c.Resource = []ScopeSelection{*rule.Resource}
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

// narrowRule narrows a rule by the constraint's Scopes, one side at a time.
// A side the rule's action can't take is skipped. A validation error means the rule doesn't apply through the binding:
//
//   - the action takes no resource, so the constraint has nothing to narrow
//   - the action takes a target, the constraint sets one, and the rule has none: the rule allows only operations
//     without a target, so its set of targets is empty and stays empty when narrowed
//   - no side narrows the rule, so leaving it would grant it as written
//   - an input can't be narrowed (see narrowInput)
func narrowRule(ctx context.Context, compiled CompiledRule, rule ResolvedRule, scopes constraintScopes) (CompiledRule, error) {
	if !rule.Contract.TakesResource() {
		return CompiledRule{}, NewInvalid(ReasonConstraintDoesNotFit,
			"%s takes no resource, so the constraint has nothing to narrow", rule.Action)
	}

	narrowed := false
	if scopes.resource != nil {
		scope, err := narrowInput(rule.Contract, "resource", *rule.Resource, *scopes.resource)
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

		scope, err := narrowInput(rule.Contract, "target", *rule.Target, *scopes.target)
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
// as the rule's own Scope must be: whole-type targets for create, update and delete, and row filters for read.
func narrowInput(contract ActionContract, input string, ruleScope, constraintScope ScopeSelection) (ScopeSelection, error) {
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

	return narrowed, nil
}

// ValidateBinding validates a stored binding against its role and the Scopes its constraint names,
// and returns what it grants.
func ValidateBinding(ctx context.Context, cache *gocache.Cache, binding models.RoleBinding) (CompiledBinding, error) {
	if _, err := RoleBindingSpec(binding); err != nil {
		return CompiledBinding{}, err
	}

	role, err := bindingRole(ctx, binding)
	if err != nil {
		return CompiledBinding{}, err
	} else if role == nil {
		namespace, name, _ := BindingRoleRef(binding)
		return CompiledBinding{}, NewInvalid(ReasonRoleNotFound, "role %s/%s not found", namespace, name)
	}

	return CompileBinding(ctx, cache, binding, *role)
}

// BindingRoleRef returns the namespace and name of the role a binding grants.
func BindingRoleRef(binding models.RoleBinding) (string, string, error) {
	namespace, name, err := v1.ParseReference(binding.Namespace, binding.Role)
	if err != nil {
		return "", "", NewValidationError("role binding %s/%s: role %v", binding.Namespace, binding.Name, err)
	}
	return namespace, name, nil
}

// bindingRole returns the role a binding grants, or nil when it doesn't exist.
func bindingRole(ctx context.Context, binding models.RoleBinding) (*models.Role, error) {
	namespace, name, err := BindingRoleRef(binding)
	if err != nil {
		return nil, err
	}

	var role models.Role
	if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).Find(&role).Error; err != nil {
		return nil, err
	} else if role.ID == uuid.Nil {
		return nil, nil
	}
	return &role, nil
}

// BindingsWithUnappliedRules returns the bindings of the role, in any namespace, that some of its rules don't
// apply through.
func BindingsWithUnappliedRules(ctx context.Context, role models.Role) ([]v1.BindingUnappliedRules, error) {
	var bindings []models.RoleBinding
	if err := ctx.DB().Where("deleted_at IS NULL").
		Where("(namespace = ? AND role = ?) OR role = ?", role.Namespace, role.Name, role.Namespace+"/"+role.Name).
		Order("namespace, name").Find(&bindings).Error; err != nil {
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
			Namespace: binding.Namespace,
			Name:      binding.Name,
			Rules:     lo.Map(compiled.Unapplied, func(u UnappliedRule, _ int) string { return u.Rule }),
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

	role, err := bindingRole(ctx, binding)
	if err != nil || role == nil {
		return nil, err
	}

	compiled, err := CompileBinding(ctx, nil, binding, *role)
	if err != nil && !IsValidationError(err) {
		return nil, err
	}
	return compiled.Rules, nil
}

// ReadGrants returns, by resource type, the grant a read rule lists rows through, or for a deny rule, the rows it
// keeps from listings: the rule's Scope, narrowed by the constraint's when it has one, for each type they all select.
func (r CompiledRule) ReadGrants() map[string]rls.Grant {
	grants := map[string]rls.Grant{}
	if r.Contract.Action != policy.ActionRead || len(r.Resource) == 0 {
		return grants
	}

	grant := rls.Grant{Scope: r.Resource[0].ID}
	if len(r.Resource) > 1 {
		grant.Constraints = []string{r.Resource[1].ID}
	}

	for _, kind := range r.Resource[0].DeclaredTypes() {
		if !lo.EveryBy(r.Resource, func(scope ScopeSelection) bool { return len(scope.Selectors[kind]) > 0 }) {
			continue
		}
		grants[kind] = grant
	}

	return grants
}

// condition returns the casbin condition that matches the rule against a request's Scope membership.
// A deny rule also matches a request whose membership couldn't be read.
func (r CompiledRule) condition() (string, error) {
	ids := func(scopes []ScopeSelection) []string {
		return lo.Map(scopes, func(scope ScopeSelection, _ int) string { return scope.ID })
	}

	condition, err := dutyRBAC.RuleCondition(ids(r.Resource), ids(r.Target))
	if err != nil {
		return "", fmt.Errorf("rule %s: %w", r.Name, err)
	}

	if r.Deny {
		return dutyRBAC.DenyCondition(condition), nil
	}
	return condition, nil
}

// declaredObjects returns the RBAC objects of the resource types every Scope of the rule's resource selects,
// entirely or in part.
func (r CompiledRule) declaredObjects() []string {
	if len(r.Resource) == 0 {
		return nil
	}

	var objects []string
	for _, kind := range r.Resource[0].DeclaredTypes() {
		object, ok := objectForType[kind]
		if ok && lo.EveryBy(r.Resource, func(scope ScopeSelection) bool { return len(scope.Selectors[kind]) > 0 }) {
			objects = append(objects, object)
		}
	}

	return objects
}

// wholeTypeObjects returns the RBAC objects of the resource types that every Scope of the rule's resource
// selects entirely, e.g. catalog when every Scope has config: {name: "*"}.
func (r CompiledRule) wholeTypeObjects() []string {
	if len(r.Resource) == 0 {
		return nil
	}

	var objects []string
	for _, kind := range r.Resource[0].DeclaredTypes() {
		if object, ok := objectForType[kind]; ok && r.SelectsEntirely(kind) {
			objects = append(objects, object)
		}
	}

	return objects
}

// GrantsAllOf reports whether the rule grants every resource of the type through Scopes that never change: every
// Scope of its resource is shipped and selects the whole type. Such a grant needn't name its Scopes.
func (r CompiledRule) GrantsAllOf(kind string) bool {
	return r.SelectsEntirely(kind) && lo.EveryBy(r.Resource, func(scope ScopeSelection) bool { return scope.Shipped })
}

// SelectsEntirely reports whether every Scope of the rule's resource selects every resource of the type.
func (r CompiledRule) SelectsEntirely(kind string) bool {
	return len(r.Resource) > 0 && lo.EveryBy(r.Resource, func(scope ScopeSelection) bool {
		return lo.SomeBy(scope.Selectors[kind], func(s types.ResourceSelector) bool { return s.Wildcard() })
	})
}

// compiledRuleToCasbinRules compiles a rule of a binding into casbin policies filed under the binding's principal.
//
// A rule checked on the resources of each request is matched by the Scopes the request's resources are in:
//
//	p, binding:<ns>/<name>, *, <action>, allow, <condition over r.obj.Membership>, binding:<binding id>/<rule>
//
// A rule checked on whole object types is granted on the object of each type it selects entirely, e.g. catalog.
// So is a read rule, for endpoints that check reads on whole object types.
//
// A rule whose action takes no resource is granted on the action's object, e.g. events for event:read.
//
// A deny rule compiles the same way with the deny effect, except that a read rule is denied on the object of every
// type it selects, entirely or in part. A subject denied some resources of a type can't read the whole type, so it
// reads it the way a subject granted some of it does: through listings filtered by row.
func compiledRuleToCasbinRules(principal string, rule CompiledRule) ([][]string, error) {
	effect := lo.Ternary(rule.Deny, "deny", "allow")

	if !rule.Contract.TakesResource() {
		return [][]string{{"p", principal, rule.Contract.Object, rule.Contract.Action, effect, "", rule.ID}}, nil
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
		objects := rule.wholeTypeObjects()
		if rule.Deny && rule.Contract.enforcement == enforcedWithRows {
			objects = rule.declaredObjects()
		}

		for _, object := range objects {
			policies = append(policies, []string{"p", principal, object, rule.Contract.Action, effect, "", rule.ID})
		}
	}

	return policies, nil
}
