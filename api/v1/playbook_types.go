package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type Permission struct {
	Role string `json:"role,omitempty" yaml:"role,omitempty"`
	Team string `json:"team,omitempty" yaml:"team,omitempty"`
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

// PlaybookResourceFilter defines a filter that decides whether a resource (config or a component)
// is permitted be run on the Playbook.
type PlaybookResourceFilter struct {
	Type string            `json:"type,omitempty" yaml:"type,omitempty"`
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// PlaybookParameter defines a parameter that a playbook needs to run.
type PlaybookParameter struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty"`
	Label string `json:"label,omitempty" yaml:"label,omitempty"`
}

type PlaybookSpec struct {
	Description string                   `json:"description,omitempty" yaml:"description,omitempty"`
	Permissions []Permission             `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Configs     []PlaybookResourceFilter `json:"configs,omitempty" yaml:"configs,omitempty"`
	Components  []PlaybookResourceFilter `json:"components,omitempty" yaml:"components,omitempty"`
	Parameters  []PlaybookParameter      `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Actions     []PlaybookAction         `json:"actions" yaml:"actions"`
}

// PlaybookStatus defines the observed state of Playbook
type PlaybookStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Playbook is the schema for the Playbooks API
type Playbook struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlaybookSpec   `json:"spec,omitempty"`
	Status PlaybookStatus `json:"status,omitempty"`
}
