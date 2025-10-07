package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AccessScopeSubject defines the human subject for access scope
type AccessScopeSubject struct {
	// Email of the person
	Person string `json:"person,omitempty"`

	// Name of the team
	Team string `json:"team,omitempty"`
}

func (t *AccessScopeSubject) Empty() bool {
	return t.Person == "" && t.Team == ""
}

// AccessScopeScope defines the visibility boundaries
type AccessScopeScope struct {
	// Tags that resources must match (AND logic for multiple tags)
	Tags map[string]string `json:"tags,omitempty"`

	// Agents that resources can belong to (OR logic)
	Agents []string `json:"agents,omitempty"`

	// Names - exact matches only. Use ["*"] for all names
	Names []string `json:"names,omitempty"`
}

func (t *AccessScopeScope) Empty() bool {
	return len(t.Tags) == 0 && len(t.Agents) == 0 && len(t.Names) == 0
}

// AccessScopeResourceType defines the valid resource types for AccessScope
// +kubebuilder:validation:Enum=*;config;component;playbook;canary
type AccessScopeResourceType string

const (
	AccessScopeResourceAll       AccessScopeResourceType = "*"
	AccessScopeResourceConfig    AccessScopeResourceType = "config"
	AccessScopeResourceComponent AccessScopeResourceType = "component"
	AccessScopeResourcePlaybook  AccessScopeResourceType = "playbook"
	AccessScopeResourceCanary    AccessScopeResourceType = "canary"
)

// Logic Notes:
// - Multiple fields within a SINGLE AccessScopeScope use AND logic (tags AND agents AND names)
// - Multiple AccessScopeScope items in Scopes array use OR logic (scope[0] OR scope[1] OR scope[2])
// - Multiple AccessScope resources use OR logic

// +kubebuilder:object:generate=true
type AccessScopeSpec struct {
	// Description provides a brief explanation of the access scope
	Description string `json:"description,omitempty"`

	// Subject defines the human entity (person or team) to which the scope applies
	// +kubebuilder:validation:Required
	Subject AccessScopeSubject `json:"subject"`

	// Resources specifies which resource types this scope applies to
	// Use ["*"] for all resource types
	// Valid values: config, component, playbook, canary
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Resources []AccessScopeResourceType `json:"resources"`

	// Scopes defines the visibility boundaries using tags, agents, and/or names
	// Multiple scopes in this array use OR logic (scope[0] OR scope[1])
	// Multiple fields within a single scope use AND logic (tags AND agents AND names)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Scopes []AccessScopeScope `json:"scopes"`
}

type AccessScopeStatus struct {
	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the AccessScope's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
//
// AccessScope is the Schema for defining visibility boundaries for human subjects
type AccessScope struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   AccessScopeSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status AccessScopeStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
//
// AccessScopeList contains a list of AccessScope
type AccessScopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccessScope `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccessScope{}, &AccessScopeList{})
}
