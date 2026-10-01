package v1

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// Role is a named set of rules, each allowing or denying an action on the resources selected by a Scope,
// optionally on the targets selected by another Scope.
// A role grants nothing on its own: a RoleBinding grants it to subjects.
// +kubebuilder:validation:XValidation:rule="!(self.metadata.name in ['admin','everyone','guest','viewer','editor','commander','responder','agent'])",message="a Role can't be named after a built-in role"
type Role struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   RoleSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status RoleStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:generate=true
type RoleSpec struct {
	// Description provides a brief explanation of the role.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// +listType=map
	// +listMapKey=name

	// Rules of the role. A matching deny rule overrides every allow rule.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	Rules []RoleRule `json:"rules" yaml:"rules"`
}

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.action in ['read','create','update','delete','playbook:run','playbook:approve','playbook:cancel','mcp:run'] || self.action.matches('^invoke:[^:*!, ]+:[^:*!, ]+$')",message="action must be a known action or invoke:<plugin>:<operation>, without patterns"
// +kubebuilder:validation:XValidation:rule="!(has(self.deny) && self.deny && self.action == 'read')",message="deny rules on read aren't supported"
// +kubebuilder:validation:XValidation:rule="!has(self.target) || self.action in ['playbook:run','playbook:approve','playbook:cancel']",message="only playbook:run, playbook:approve and playbook:cancel take a target"
type RoleRule struct {
	// Name identifies the rule within the role. RoleBinding constraints reference rules by name.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name" yaml:"name"`

	// Description provides a brief explanation of the rule.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Action is the action the rule allows or denies, e.g. read or playbook:run.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Action string `json:"action" yaml:"action"`

	// Resource is the Scope selecting the resources the action is performed on.
	Resource ScopeReference `json:"resource" yaml:"resource"`

	// Target is the Scope selecting what the action is performed on the resource with,
	// e.g. the configs a playbook runs on. Only actions that take a target accept it.
	// Without a target, the rule only matches operations without one.
	Target *ScopeReference `json:"target,omitempty" yaml:"target,omitempty"`

	// Deny makes the rule deny the action instead of allowing it.
	Deny bool `json:"deny,omitempty" yaml:"deny,omitempty"`
}

// ScopeReference references a Scope in the namespace of the Role or RoleBinding.
// +kubebuilder:object:generate=true
type ScopeReference struct {
	// ScopeRef is the name of the Scope.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ScopeRef string `json:"scopeRef" yaml:"scopeRef"`
}

var ruleNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func (t RoleSpec) Validate() error {
	if len(t.Rules) == 0 {
		return fmt.Errorf("a role must have at least one rule")
	}

	names := map[string]struct{}{}
	for i, rule := range t.Rules {
		if len(rule.Name) > 63 || !ruleNamePattern.MatchString(rule.Name) {
			return fmt.Errorf("rule %d: name %q must be a lowercase DNS label", i, rule.Name)
		}

		if _, ok := names[rule.Name]; ok {
			return fmt.Errorf("rule %s: rule names must be unique", rule.Name)
		}
		names[rule.Name] = struct{}{}

		if strings.TrimSpace(rule.Action) == "" {
			return fmt.Errorf("rule %s: action is required", rule.Name)
		}

		if rule.Resource.ScopeRef == "" {
			return fmt.Errorf("rule %s: resource.scopeRef is required", rule.Name)
		}

		if rule.Target != nil && rule.Target.ScopeRef == "" {
			return fmt.Errorf("rule %s: target.scopeRef is required when target is set", rule.Name)
		}
	}

	return nil
}

type RoleStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*Role)(nil)
var _ kopper.StatusConditioner = (*Role)(nil)
var _ kopper.ObservedGenerationSetter = (*Role)(nil)

func (t *Role) SetObservedGeneration(generation int64) {
	t.Status.ObservedGeneration = generation
}

func (t *Role) GetObservedGeneration() int64 {
	return t.Status.ObservedGeneration
}

func (t *Role) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *Role) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*Role)
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
// RoleList contains a list of Role
type RoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Role `json:"items"`
}
