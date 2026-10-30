package v1

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// RoleBinding grants a Role to many subjects, or with deny, refuses it to them,
// optionally narrowing the role's rules with a constraint.
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

	// Role is the name of the Role to grant, in the binding's namespace, or namespace/name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?/)?[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Role string `json:"role" yaml:"role"`

	// Subjects the role is granted to. At least one is required.
	// +kubebuilder:validation:XValidation:rule="[has(self.people) && size(self.people) > 0, has(self.teams) && size(self.teams) > 0, has(self.notifications) && size(self.notifications) > 0, has(self.playbooks) && size(self.playbooks) > 0, has(self.topologies) && size(self.topologies) > 0, has(self.scrapers) && size(self.scrapers) > 0, has(self.canaries) && size(self.canaries) > 0, has(self.plugins) && size(self.plugins) > 0].exists(x, x)",message="at least one subject is required"
	Subjects RoleBindingSubjects `json:"subjects" yaml:"subjects"`

	// Constraint narrows every rule of the role for this binding's subjects.
	// Without one, the binding grants, or denies, the role's rules as written.
	Constraint *RoleBindingConstraint `json:"constraint,omitempty" yaml:"constraint,omitempty"`

	// Deny refuses the subjects every operation the role's rules, narrowed by the constraint, match,
	// whatever any other binding grants them. A deny binding never applies to RoleBindings.
	Deny bool `json:"deny,omitempty" yaml:"deny,omitempty"`
}

// RoleBindingConstraint narrows every rule of the bound role, one side at a time:
// an operation's resource, and its target, must also belong to the constraint's Scopes.
// A rule the constraint can't narrow doesn't apply through the binding, and is reported by its AllRulesApply condition.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.resource) || has(self.target)",message="a constraint must set resource or target"
type RoleBindingConstraint struct {
	// Resource is a Scope the resource of every operation must also belong to.
	Resource *ScopeReference `json:"resource,omitempty" yaml:"resource,omitempty"`

	// Target is a Scope the target of every operation must also belong to.
	Target *ScopeReference `json:"target,omitempty" yaml:"target,omitempty"`
}

// +kubebuilder:object:generate=true
type RoleBindingSubjects struct {
	PermissionGroupSubjects `json:",inline" yaml:",inline"`

	// Deprecated: built-in roles were removed, so a binding that sets roles is rejected. Select teams instead.
	// +kubebuilder:validation:MaxItems=0
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Plugins selects plugins by the namespace and name of their Plugin resource.
	// A binding that selects one can only grant read on connections.
	Plugins []PermissionGroupSelector `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

func (t RoleBindingSubjects) Empty() bool {
	s := t.PermissionGroupSubjects
	return len(t.Plugins) == 0 && len(s.People) == 0 && len(s.Teams) == 0 &&
		len(s.Notifications) == 0 && len(s.Playbooks) == 0 && len(s.Topologies) == 0 &&
		len(s.Scrapers) == 0 && len(s.Canaries) == 0
}

// ParseProviderPerson splits a people entry naming a user of an external identity provider, <provider>:<username>,
// on its first ':'. ok is false for any other entry, e.g. an email address.
func ParseProviderPerson(entry string) (provider, username string, ok bool) {
	return strings.Cut(entry, ":")
}

func (t RoleBindingSpec) Validate() error {
	if strings.TrimSpace(t.Role) == "" {
		return fmt.Errorf("role is required")
	} else if err := validateReference(t.Role); err != nil {
		return fmt.Errorf("role %w", err)
	}

	if c := t.Constraint; c != nil {
		if c.Resource == nil && c.Target == nil {
			return fmt.Errorf("constraint must set resource or target")
		}

		if c.Resource != nil && c.Resource.ScopeRef == "" {
			return fmt.Errorf("constraint: resource.scopeRef is required when resource is set")
		} else if c.Resource != nil {
			if err := validateReference(c.Resource.ScopeRef); err != nil {
				return fmt.Errorf("constraint: resource.scopeRef %w", err)
			}
		}

		if c.Target != nil && c.Target.ScopeRef == "" {
			return fmt.Errorf("constraint: target.scopeRef is required when target is set")
		} else if c.Target != nil {
			if err := validateReference(c.Target.ScopeRef); err != nil {
				return fmt.Errorf("constraint: target.scopeRef %w", err)
			}
		}
	}

	if len(t.Subjects.Roles) > 0 {
		return fmt.Errorf("subjects.roles is no longer supported: built-in roles were removed, select teams instead")
	}

	if t.Subjects.Empty() {
		return fmt.Errorf("at least one subject is required")
	}

	for _, person := range t.Subjects.People {
		if person == "*" {
			if len(t.Subjects.People) > 1 {
				return fmt.Errorf(`people: "*" selects every person and must be the only entry`)
			}
			continue
		}

		if provider, username, ok := ParseProviderPerson(person); ok {
			if errs := validation.IsDNS1123Subdomain(provider); len(errs) > 0 {
				return fmt.Errorf("person %q must name an external identity provider before ':': %s", person, strings.Join(errs, ", "))
			} else if username == "" {
				return fmt.Errorf("person %q must have a username after ':', or '*' for every user of the provider", person)
			} else if username != "*" && strings.Contains(username, "*") {
				return fmt.Errorf(`person %q: patterns aren't supported, use "%s:*" to select every user of the provider`, person, provider)
			}
			continue
		}

		if strings.Contains(person, "*") {
			return fmt.Errorf(`person %q must be an email address; patterns aren't supported, use "*" alone to select every person`, person)
		}

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
		"plugins":       t.Subjects.Plugins,
	} {
		for i, selector := range selectors {
			if err := validateResourceSubject(selector); err != nil {
				return fmt.Errorf("%s subject %d: %w", kind, i, err)
			}
		}
	}

	return nil
}

// validateResourceSubject checks a resource subject: at least one of namespace and name, where name is an exact value
// or "*", and namespace an exact value. Patterns aren't supported; "," and "!" are ordinary characters.
func validateResourceSubject(selector PermissionGroupSelector) error {
	if selector.Empty() {
		return fmt.Errorf("namespace or name is required")
	}

	if selector.Name != "*" && strings.Contains(selector.Name, "*") {
		return fmt.Errorf(`name %q must be an exact value, or "*"`, selector.Name)
	}

	if strings.Contains(selector.Namespace, "*") {
		return fmt.Errorf("namespace %q must be an exact value; omit it to match any namespace", selector.Namespace)
	}

	return nil
}

// ConditionAllRulesApply is the condition of a RoleBinding that reports whether every rule of its role
// applies through it. It's False, naming each rule that doesn't and why, when the constraint can't narrow a rule.
const ConditionAllRulesApply = "AllRulesApply"

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
