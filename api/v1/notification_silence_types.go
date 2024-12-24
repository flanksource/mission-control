package v1

import (
	"github.com/flanksource/duty/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// NotificationStatus defines the observed state of Notification
type NotificationSilenceStatus struct {
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
