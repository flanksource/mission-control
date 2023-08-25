package v1

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

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
	Name  string `json:"name" yaml:"name"`
	Label string `json:"label" yaml:"label"`
}

type PlaybookApprovers struct {
	// Emails of the approvers
	People []string `json:"people,omitempty" yaml:"people,omitempty"`

	// Names of the teams
	Teams []string `json:"teams,omitempty" yaml:"teams,omitempty"`
}

func (t *PlaybookApprovers) Empty() bool {
	return len(t.People) == 0 && len(t.Teams) == 0
}

type PlaybookApprovalType string

const (
	// PlaybookApprovalTypeAny means just a single approval can suffice.
	PlaybookApprovalTypeAny PlaybookApprovalType = "any"

	// PlaybookApprovalTypeAll means all approvals are required
	PlaybookApprovalTypeAll PlaybookApprovalType = "all"
)

type PlaybookApproval struct {
	Type      PlaybookApprovalType `json:"type,omitempty" yaml:"type,omitempty"`
	Approvers PlaybookApprovers    `json:"approvers,omitempty" yaml:"approvers,omitempty"`
}

type PlaybookSpec struct {
	Description string                   `json:"description,omitempty" yaml:"description,omitempty"`
	Permissions []Permission             `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Configs     []PlaybookResourceFilter `json:"configs,omitempty" yaml:"configs,omitempty"`
	Components  []PlaybookResourceFilter `json:"components,omitempty" yaml:"components,omitempty"`
	Parameters  []PlaybookParameter      `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Actions     []PlaybookAction         `json:"actions" yaml:"actions"`
	Approval    *PlaybookApproval        `json:"approval,omitempty" yaml:"approval,omitempty"`
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

func PlaybookFromModel(p models.Playbook) (Playbook, error) {
	var spec PlaybookSpec
	if err := json.Unmarshal(p.Spec, &spec); err != nil {
		return Playbook{}, nil
	}

	out := Playbook{
		ObjectMeta: metav1.ObjectMeta{
			Name:              p.Name,
			UID:               types.UID(p.ID.String()),
			CreationTimestamp: metav1.Time{Time: p.CreatedAt},
		},
		Spec: spec,
	}

	return out, nil
}

// +kubebuilder:object:root=true

// PlaybookList contains a list of Playbook
type PlaybookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Playbook `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Playbook{}, &PlaybookList{})
}
