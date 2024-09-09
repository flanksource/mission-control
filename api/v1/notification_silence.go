package v1

import (
	"github.com/flanksource/duty/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true
type NotificationSilenceSpec struct {
	Matcher   *types.CelExpression `json:"matcher,omitempty"`
	From      metav1.Time          `json:"from"`
	Until     metav1.Time          `json:"until"`
	Recursive bool                 `json:"recursive,omitempty"`
}

// NotificationSilence defines the observed state of Notification Silence
type NotificationSilenceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Notification is the Schema for the Notification API
type NotificationSilence struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   NotificationSilenceSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status NotificationSilenceStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NotificationList contains a list of Notification
type NotificationSilenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NotificationSilence `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationSilence{}, &NotificationSilenceList{})
}
