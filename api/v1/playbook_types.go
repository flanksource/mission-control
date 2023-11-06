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
	Name        string            `json:"name" yaml:"name"`
	Label       string            `json:"label" yaml:"label"`
	Required    bool              `json:"required,omitempty" yaml:"required,omitempty"`
	Icon        string            `json:"icon,omitempty" yaml:"icon,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Type        string            `json:"type,omitempty" yaml:"type,omitempty"`
	Properties  map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`
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

type PlaybookEventDetail struct {
	// Labels specifies the key-value pairs that the associated event's resource must match.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// CEL expression for additional event filtering.
	Filter string `json:"filter,omitempty" yaml:"filter,omitempty"`

	// Event to listen for.
	Event string `json:"event" yaml:"event"`
}

// PlaybookEvent defines the list of supported events to trigger a playbook.
type PlaybookEvent struct {
	Canary    []PlaybookEventDetail `json:"canary,omitempty" yaml:"canary,omitempty"`
	Component []PlaybookEventDetail `json:"component,omitempty" yaml:"component,omitempty"`
}

type PlaybookSpec struct {
	// Short description of the playbook.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`

	// `On` defines events that will automatically trigger the playbook.
	// If multiple events are defined, only one of those events needs to occur to trigger the playbook.
	// If multiple triggering events occur at the same time, multiple playbook runs will be triggered.
	On PlaybookEvent `json:"on,omitempty" yaml:"on,omitempty"`

	// Permissions ...
	Permissions []Permission `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// Configs filters what config items can run on this playbook.
	Configs []PlaybookResourceFilter `json:"configs,omitempty" yaml:"configs,omitempty"`

	// Checks filters what checks can run on this playbook.
	Checks []PlaybookResourceFilter `json:"checks,omitempty" yaml:"checks,omitempty"`

	// Components what components can run on this playbook.
	Components []PlaybookResourceFilter `json:"components,omitempty" yaml:"components,omitempty"`

	// Define and document what parameters are required to run this playbook.
	Parameters []PlaybookParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`

	// List of actions that need to be executed by this playbook.
	Actions []PlaybookAction `json:"actions" yaml:"actions"`

	// Approval defines the individuals and teams authorized to approve runs of this playbook.
	Approval *PlaybookApproval `json:"approval,omitempty" yaml:"approval,omitempty"`
}

// PlaybookStatus defines the observed state of Playbook
type PlaybookStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Playbook is the schema for the Playbooks API
type Playbook struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   PlaybookSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status PlaybookStatus `json:"status,omitempty" yaml:"status,omitempty"`
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
