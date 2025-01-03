package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// PermissionGroup is the Schema for the Mission Control Permission Groups
type PermissionGroup struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   PermissionGroupSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status PermissionGroupStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:generate=true
type PermissionGroupSpec struct {
	PermissionGroupSubjects `json:",inline" yaml:",inline"`

	// Name for the group
	Name string `json:"name"`
}

type PermissionGroupStatus struct {
}

// +kubebuilder:object:generate=true
type PermissionGroupSubjects struct {
	Notifications []PermissionGroupSelector `json:"notifications,omitempty"`
	People        []string                  `json:"people,omitempty"`
	Teams         []string                  `json:"teams,omitempty"`
}

// +kubebuilder:object:generate=true
type PermissionGroupSelector struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

func (t PermissionGroupSelector) Empty() bool {
	return t.Name == "" && t.Namespace == ""
}

// +kubebuilder:object:root=true
//
// PermissionGroupList contains a list of PermissionGroup
type PermissionGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PermissionGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PermissionGroup{}, &PermissionGroupList{})
}
