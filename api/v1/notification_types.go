package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NotificationRecipientSpec struct {
	// ID or email of the person
	Person string `json:"person,omitempty" yaml:"person,omitempty"`

	// name or ID of the recipient team
	Team string `json:"team,omitempty" yaml:"team,omitempty"`

	// Email of the recipient
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// Specify connection string for an external service.
	// Should be in the format of connection://<type>/name
	// or the id of the connection.
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`

	// Specify shoutrrr URL
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// Properties for Shoutrrr
	Properties map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`
}

// Empty returns true if none of the receivers are set
func (t *NotificationRecipientSpec) Empty() bool {
	return t.Person == "" && t.Team == "" && t.Email == "" && t.Connection == "" && t.URL == ""
}

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

	// RepeatInterval is the waiting time to resend a notification after it has been succefully sent.
	RepeatInterval string `json:"repeatInterval,omitempty" yaml:"repeatInterval,omitempty"`

	// RepeatGroup allows notifications to be grouped by certain set of keys and only send
	// one per group within the specified repeat interval.
	RepeatGroup []string `json:"repeatGroup,omitempty" yaml:"repeatGroup,omitempty"`

	// Specify the recipient
	To NotificationRecipientSpec `json:"to" yaml:"to"`
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
