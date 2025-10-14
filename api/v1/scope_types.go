package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ScopeResourceSelector is a subset of ResourceSelector used for defining scope targets
// +kubebuilder:object:generate=true
type ScopeResourceSelector struct {
	// Agent can be the agent id or the name of the agent.
	Agent string `yaml:"agent,omitempty" json:"agent,omitempty"`

	// Name is the name of the resource.
	// Supports special wildcard directive '*' which matches any resource.
	// NOTE: Prefix and suffix wildcards (e.g. 'nginx-*') are NOT supported.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// TagSelector selects resources by tags using label selector syntax
	// Example: "env=prod,region=us-west"
	TagSelector string `yaml:"tagSelector,omitempty" json:"tagSelector,omitempty"`
}

// ScopeTarget defines a single target in a Scope
// Each target should contain exactly ONE resource type
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="[has(self.config), has(self.component), has(self.playbook), has(self.canary), has(self.global)].filter(x, x).size() == 1",message="exactly one of config, component, playbook, canary, or global must be specified"
type ScopeTarget struct {
	// Config selector (mutually exclusive with other resource types in practice)
	Config *ScopeResourceSelector `json:"config,omitempty"`

	// Component selector
	Component *ScopeResourceSelector `json:"component,omitempty"`

	// Playbook selector
	Playbook *ScopeResourceSelector `json:"playbook,omitempty"`

	// Canary selector
	Canary *ScopeResourceSelector `json:"canary,omitempty"`

	// Global selector - applies to all resource types (wildcard)
	Global *ScopeResourceSelector `json:"global,omitempty"`
}

// +kubebuilder:object:generate=true
type ScopeSpec struct {
	// Description provides a brief explanation of what this scope covers
	Description string `json:"description,omitempty"`

	// Targets defines the resource selectors for this scope
	// Each target should contain exactly one resource type
	// Multiple targets are combined with OR logic
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Targets []ScopeTarget `json:"targets"`
}

type ScopeStatus struct {
	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Scope's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
//
// Scope defines a collection of resources of a single type for access control
type Scope struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   ScopeSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status ScopeStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
//
// ScopeList contains a list of Scope
type ScopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Scope `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Scope{}, &ScopeList{})
}
