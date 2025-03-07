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
	// Deprecated: Use metadata.name instead
	Name string `json:"name,omitempty"`
}

type PermissionGroupStatus struct {
}

// +kubebuilder:object:generate=true
type PermissionGroupSubjects struct {
	Notifications []PermissionGroupSelector `json:"notifications,omitempty"`
	Playbooks     []PermissionGroupSelector `json:"playbooks,omitempty"`
	Topologies    []PermissionGroupSelector `json:"topologies,omitempty"`
	Scrapers      []PermissionGroupSelector `json:"scrapers,omitempty"`
	Canaries      []PermissionGroupSelector `json:"canaries,omitempty"`

	// List of ids and email of people.
	// To select all users, use the wildcard selector: ["*"]
	People []string `json:"people,omitempty"`

	// Teams is a list of team names
	Teams []string `json:"teams,omitempty"`
}

// +kubebuilder:object:generate=true
type PermissionGroupSelector struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

func (t PermissionGroupSelector) Empty() bool {
	return t.Name == "" && t.Namespace == ""
}

func (t PermissionGroupSelector) Wildcard() bool {
	return t.Name == "*" && t.Namespace == ""
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
