package v1

import (
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"sync"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/kopper"
	"github.com/google/cel-go/cel"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BindableRoles are the built-in roles a RoleBinding can select subjects by.
// Only roles no other subject field can select are allowed: viewer, editor,
// commander and responder are left out since teams select the same people,
// and binding to one would also reach every role that inherits it.
var BindableRoles = []string{
	policy.RoleEveryone,
	policy.RoleGuest,
	policy.RoleAgent,
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// RoleBinding grants a Role (in the same namespace) to many subjects,
// optionally narrowing the role's allow rules with constraints.
type RoleBinding struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   RoleBindingSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status RoleBindingStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:generate=true
type RoleBindingSpec struct {
	// Description provides a brief explanation of the binding.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Role is the name of the Role to grant, in the same namespace as the binding.
	// +kubebuilder:validation:MinLength=1
	Role string `json:"role" yaml:"role"`

	// Subjects the role is granted to. At least one is required.
	// +kubebuilder:validation:XValidation:rule="[has(self.people) && size(self.people) > 0, has(self.teams) && size(self.teams) > 0, has(self.roles) && size(self.roles) > 0, has(self.oidc) && size(self.oidc) > 0, has(self.notifications) && size(self.notifications) > 0, has(self.playbooks) && size(self.playbooks) > 0, has(self.topologies) && size(self.topologies) > 0, has(self.scrapers) && size(self.scrapers) > 0, has(self.canaries) && size(self.canaries) > 0].exists(x, x)",message="at least one subject is required"
	Subjects RoleBindingSubjects `json:"subjects" yaml:"subjects"`

	// +listType=map
	// +listMapKey=rule

	// Constraints narrow rules of the role for this binding's subjects.
	// Without constraints (empty, null or missing), the binding grants every rule of the role as defined.
	// With constraints, it grants only the allow rules they name. Deny rules always apply as written.
	Constraints []RoleBindingConstraint `json:"constraints,omitempty" yaml:"constraints,omitempty"`
}

// RoleBindingConstraint narrows one allow rule of the bound role.
// A constraint can only narrow: the rule's resource, and its target, must also belong to the constraint's Scopes.
// +kubebuilder:object:generate=true
type RoleBindingConstraint struct {
	// Rule is the name of a rule of the role.
	// +kubebuilder:validation:MinLength=1
	Rule string `json:"rule" yaml:"rule"`

	// Resource is a Scope, in the binding's namespace, the rule's resource must also belong to.
	Resource *ScopeReference `json:"resource,omitempty" yaml:"resource,omitempty"`

	// Target is a Scope, in the binding's namespace, the rule's target must also belong to.
	// Only for rules that have a target.
	Target *ScopeReference `json:"target,omitempty" yaml:"target,omitempty"`
}

// +kubebuilder:object:generate=true
type RoleBindingSubjects struct {
	PermissionGroupSubjects `json:",inline" yaml:",inline"`

	// Roles selects subjects by built-in role: everyone, guest or agent.
	// +kubebuilder:validation:items:Enum=everyone;guest;agent
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// OIDC selects users of external identity providers by the claims of their tokens.
	OIDC []OIDCSubject `json:"oidc,omitempty" yaml:"oidc,omitempty"`
}

func (t RoleBindingSubjects) Empty() bool {
	s := t.PermissionGroupSubjects
	return len(t.Roles) == 0 && len(t.OIDC) == 0 && len(s.People) == 0 && len(s.Teams) == 0 &&
		len(s.Notifications) == 0 && len(s.Playbooks) == 0 && len(s.Topologies) == 0 &&
		len(s.Scrapers) == 0 && len(s.Canaries) == 0
}

// +kubebuilder:object:generate=true
type OIDCSubject struct {
	// Provider is the name of an ExternalIdentityProvider.
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider" yaml:"provider"`

	// Match is a CEL expression over claims, the verified token's claims, that returns a bool.
	// e.g. 'operators' in claims.groups && claims.tenant == 'a'
	// An error, such as a missing claim, is no match.
	// +kubebuilder:validation:MinLength=1
	Match string `json:"match" yaml:"match"`
}

var claimsEnv = sync.OnceValues(func() (*cel.Env, error) {
	return cel.NewEnv(cel.Variable("claims", cel.MapType(cel.StringType, cel.DynType)))
})

// CompileClaimsMatch compiles a CEL expression over claims that returns a bool.
func CompileClaimsMatch(expression string) (cel.Program, error) {
	env, err := claimsEnv()
	if err != nil {
		return nil, err
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	if ast.OutputType() != cel.BoolType && ast.OutputType() != cel.DynType {
		return nil, fmt.Errorf("must return a bool, not %s", ast.OutputType())
	}

	return env.Program(ast)
}

// EvalClaimsMatch reports whether the claims satisfy a program from CompileClaimsMatch.
// An error, or a result that isn't true, is no match.
func EvalClaimsMatch(program cel.Program, claims map[string]any) bool {
	out, _, err := program.Eval(map[string]any{"claims": claims})
	if err != nil {
		return false
	}

	matched, ok := out.Value().(bool)
	return ok && matched
}

func (t RoleBindingSpec) Validate() error {
	if strings.TrimSpace(t.Role) == "" {
		return fmt.Errorf("role is required")
	}

	constrained := map[string]struct{}{}
	for i, constraint := range t.Constraints {
		if constraint.Rule == "" {
			return fmt.Errorf("constraint %d: rule is required", i)
		}

		if _, ok := constrained[constraint.Rule]; ok {
			return fmt.Errorf("constraint %d: rule %q is constrained more than once", i, constraint.Rule)
		}
		constrained[constraint.Rule] = struct{}{}

		if constraint.Resource != nil && constraint.Resource.ScopeRef == "" {
			return fmt.Errorf("constraint %d: resource.scopeRef is required when resource is set", i)
		}

		if constraint.Target != nil && constraint.Target.ScopeRef == "" {
			return fmt.Errorf("constraint %d: target.scopeRef is required when target is set", i)
		}
	}

	if t.Subjects.Empty() {
		return fmt.Errorf("at least one subject is required")
	}

	for _, person := range t.Subjects.People {
		if address, err := mail.ParseAddress(person); err != nil || address.Address != person {
			return fmt.Errorf("person %q must be an email address", person)
		}
	}

	for kind, selectors := range map[string][]PermissionGroupSelector{
		"playbooks":     t.Subjects.Playbooks,
		"notifications": t.Subjects.Notifications,
		"topologies":    t.Subjects.Topologies,
		"scrapers":      t.Subjects.Scrapers,
		"canaries":      t.Subjects.Canaries,
	} {
		for i, selector := range selectors {
			if err := validateResourceSubject(selector); err != nil {
				return fmt.Errorf("%s subject %d: %w", kind, i, err)
			}
		}
	}

	for _, role := range t.Subjects.Roles {
		if !slices.Contains(BindableRoles, role) {
			return fmt.Errorf("role %q cannot be bound. Must be one of %v", role, BindableRoles)
		}
	}

	for i, subject := range t.Subjects.OIDC {
		if strings.TrimSpace(subject.Provider) == "" {
			return fmt.Errorf("oidc subject %d: provider is required", i)
		}

		if _, err := CompileClaimsMatch(subject.Match); err != nil {
			return fmt.Errorf("oidc subject %d: invalid match %q: %w", i, subject.Match, err)
		}
	}

	return nil
}

// validateResourceSubject checks a resource subject: at least one of namespace and name, where name is an exact value
// or "*", and namespace an exact value. Patterns, lists and exclusions aren't supported.
func validateResourceSubject(selector PermissionGroupSelector) error {
	if selector.Empty() {
		return fmt.Errorf("namespace or name is required")
	}

	if selector.Name != "*" && strings.ContainsAny(selector.Name, "*!,") {
		return fmt.Errorf(`name %q must be an exact value, or "*"`, selector.Name)
	}

	if strings.ContainsAny(selector.Namespace, "*!,") {
		return fmt.Errorf("namespace %q must be an exact value; omit it to match any namespace", selector.Namespace)
	}

	return nil
}

type RoleBindingStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*RoleBinding)(nil)
var _ kopper.StatusConditioner = (*RoleBinding)(nil)
var _ kopper.ObservedGenerationSetter = (*RoleBinding)(nil)

func (t *RoleBinding) SetObservedGeneration(generation int64) {
	t.Status.ObservedGeneration = generation
}

func (t *RoleBinding) GetObservedGeneration() int64 {
	return t.Status.ObservedGeneration
}

func (t *RoleBinding) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *RoleBinding) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*RoleBinding)
	if !ok {
		return nil
	}

	if cmp.Diff(t.Status, og.Status) == "" {
		return nil
	}

	clientObj, ok := original.(client.Object)
	if !ok {
		return nil
	}

	return client.MergeFrom(clientObj)
}

// +kubebuilder:object:root=true
//
// RoleBindingList contains a list of RoleBinding
type RoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RoleBinding `json:"items"`
}
