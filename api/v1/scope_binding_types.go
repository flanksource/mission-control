package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ScopeBindingSubjects defines the human subjects for the binding
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="size(self.persons) > 0 || size(self.teams) > 0",message="at least one person or team must be specified"
type ScopeBindingSubjects struct {
	// Persons is a list of person emails
	// +kubebuilder:validation:MaxItems=10
	Persons []string `json:"persons,omitempty"`

	// Teams is a list of team names
	// +kubebuilder:validation:MaxItems=10
	Teams []string `json:"teams,omitempty"`
}

// Empty returns true if no subjects are defined
func (s *ScopeBindingSubjects) Empty() bool {
	return len(s.Persons) == 0 && len(s.Teams) == 0
}

// +kubebuilder:object:generate=true
type ScopeBindingSpec struct {
	// Description provides a brief explanation of this binding
	Description string `json:"description,omitempty"`

	// Subjects defines the persons or teams this binding applies to
	// +kubebuilder:validation:Required
	Subjects ScopeBindingSubjects `json:"subjects"`

	// Scopes is a list of Scope names in the same namespace
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Scopes []string `json:"scopes"`
}

type ScopeBindingStatus struct {
	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the ScopeBinding's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
//
// ScopeBinding binds human subjects to a set of Scopes
type ScopeBinding struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   ScopeBindingSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status ScopeBindingStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
//
// ScopeBindingList contains a list of ScopeBinding
type ScopeBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScopeBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScopeBinding{}, &ScopeBindingList{})
}
