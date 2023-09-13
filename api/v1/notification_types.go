package v1

import (
	"github.com/flanksource/incident-commander/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true
type NotificationSpec struct {
	// List of events that can trigger this notification
	Events []string `json:"events" yaml:"events"`

	// The title for the notification
	Title string `json:"title,omitempty" yaml:"title,omitempty"`

	// Template is the notification body in markdown
	Template string `json:"template,omitempty" yaml:"template,omitempty"`

	// Cel-expression used to decide whether this notification client should send the notification
	Filter string `json:"filter,omitempty" yaml:"filter,omitempty"`

	// email or ID of the recipient person
	Person string `json:"person,omitempty" yaml:"person,omitempty"`

	// name or ID of the recipient team
	Team string `json:"team,omitempty" yaml:"team,omitempty"`

	// Custom services to send this notification to
	CustomServices []api.NotificationConfig `json:"custom_services,omitempty" yaml:"custom_services,omitempty"`

	// Properties for Shoutrrr
	Properties map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`
}

// NotificationStatus defines the observed state of Notification
type NotificationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Notification is the Schema for the Notification API
type Notification struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   NotificationSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status NotificationStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NotificationList contains a list of Notification
type NotificationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Notification `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Notification{}, &NotificationList{})
}
