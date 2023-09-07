package v1

import (
	"github.com/flanksource/incident-commander/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IncidentRuleStatus defines the observed state of IncidentRule
type IncidentRuleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// IncidentRule is the Schema for the IncidentRule API
type IncidentRule struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   api.IncidentRuleSpec `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status IncidentRuleStatus   `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IncidentRuleList contains a list of IncidentRule
type IncidentRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IncidentRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IncidentRule{}, &IncidentRuleList{})
}
