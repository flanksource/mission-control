package v1

import (
	"github.com/flanksource/duty/types"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=true
type NotificationSilenceSpec struct {
	Description *string `json:"description,omitempty"`

	// From time in RFC3339 format or just datetime
	From *string `json:"from,omitempty"`

	// Until time in RFC3339 format or just datetime
	Until *string `json:"until,omitempty"`

	Recursive bool `json:"recursive,omitempty"`

	// Filter evaluates whether to apply the silence. When provided, silence is applied only if filter evaluates to true
	Filter types.CelExpression `json:"filter,omitempty"`

	// List of resource selectors
	Selectors []types.ResourceSelector `json:"selectors,omitempty"`
}

// NotificationSilenceStatus defines the observed state of NotificationSilence
type NotificationSilenceStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*NotificationSilence)(nil)
var _ kopper.StatusConditioner = (*NotificationSilence)(nil)

func (t *NotificationSilence) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *NotificationSilence) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*NotificationSilence)
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// NotificationSilence is the Schema for the managed Notification Silences
type NotificationSilence struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NotificationSilenceSpec   `json:"spec,omitempty"`
	Status NotificationSilenceStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NotificationSilenceList contains a list of Notification Silences
type NotificationSilenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NotificationSilence `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationSilence{}, &NotificationSilenceList{})
}
