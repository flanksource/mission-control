package v1

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IncidentRuleStatus defines the observed state of IncidentRule
type IncidentRuleStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*IncidentRule)(nil)
var _ kopper.StatusConditioner = (*IncidentRule)(nil)

func (t *IncidentRule) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *IncidentRule) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*IncidentRule)
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
