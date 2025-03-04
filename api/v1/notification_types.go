package v1

import (
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// Name or <namespace>/<name> of the playbook to run.
	// When a playbook is set as the recipient, a run is triggered.
	Playbook *string `json:"playbook,omitempty" yaml:"playbook,omitempty"`
}

// Empty returns true if none of the receivers are set
func (t *NotificationRecipientSpec) Empty() bool {
	return t.Person == "" && t.Team == "" && t.Email == "" && t.Connection == "" && t.URL == "" && t.Playbook == nil
}

type NotificationFallback struct {
	NotificationRecipientSpec `json:",inline" yaml:",inline"`

	// wait this long before considering a send a failure
	Delay string `json:"delay,omitempty" yaml:"delay,omitempty"`
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

	// Specify the recipient
	To NotificationRecipientSpec `json:"to" yaml:"to"`

	// In case of failure, send the notification to this recipient
	Fallback *NotificationFallback `json:"fallback,omitempty" yaml:"fallback,omitempty"`

	// WaitFor defines a duration to delay sending a health-based notification.
	// After this period, the health status is reassessed to confirm it hasn't
	// changed, helping prevent false alarms from transient issues.
	//
	// The delay allows time for self-recovery or temporary fluctuations to
	// resolve, reducing unnecessary alerts.
	//
	// If specified, it should be a valid duration string (e.g., "5m", "1h").
	WaitFor *string `json:"waitFor,omitempty" yaml:"waitFor,omitempty"`

	// WaitForEvalPeriod defines an additional delay following the waitFor period.
	// After waitFor completes, the system actively re-scrapes the resource
	// and then waits this duration before final evaluation, unlike waitFor which
	// simply delays without re-scraping.
	//
	// Only applies to Kubernetes health notifications and cannot be turned off.
	// Defaults to 30s.
	//
	// Format: duration string (e.g., "30s", "2m")
	WaitForEvalPeriod *string `json:"waitForEvalPeriod,omitempty" yaml:"waitForEvalPeriod,omitempty"`

	// GroupBy allows notifications in waiting status to be grouped together
	// based on certain set of keys.
	//
	// Valid keys: type, description, status_reason or
	// labels & tags in the format `label:<key>` or `tag:<key>`
	GroupBy []string `json:"groupBy,omitempty"`

	// Inhibit controls notification suppression for related resources.
	// It uses the repeat interval as the window for suppression
	// as well as the wait for period.
	Inibhit []NotificationInihibition `json:"inhibit,omitempty"`
}

type NotificationInihibition struct {
	// Direction specifies the traversal direction in relation to the "From" resource.
	// - "outgoing": Looks for child resources originating from the "From" resource.
	//   Example: If "From" is "Kubernetes::Deployment", "To" could be ["Kubernetes::Pod", "Kubernetes::ReplicaSet"].
	// - "incoming": Looks for parent resources related to the "From" resource.
	//   Example: If "From" is "Kubernetes::Deployment", "To" could be ["Kubernetes::HelmRelease", "Kubernetes::Namespace"].
	// - "all": Considers both incoming and outgoing relationships.
	Direction string `json:"direction"`

	// Soft indicates whether this inhibition is advisory or strictly enforced.
	Soft bool `json:"soft,omitempty"`

	// Depth defines how many levels of child or parent resources to traverse.
	Depth int `json:"depth"`

	// From specifies the starting resource type (for example, "Kubernetes::Deployment").
	From string `json:"from"`

	// To specifies the target resource types, which are determined based on the Direction.
	// Example:
	//   - If Direction is "outgoing", these are child resources.
	//   - If Direction is "incoming", these are parent resources.
	To []string `json:"to"`
}

var NotificationReconciler kopper.Reconciler[Notification, *Notification]

// NotificationStatus defines the observed state of Notification
type NotificationStatus struct {
	Sent     int         `json:"sent,omitempty"`
	Failed   int         `json:"failed,omitempty"`
	Pending  int         `json:"pending,omitempty"`
	Status   string      `json:"status,omitempty"`
	Error    string      `json:"error,omitempty"`
	LastSent metav1.Time `json:"lastSent,omitempty"`
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

var _ kopper.StatusPatchGenerator = (*Notification)(nil)

func (t *Notification) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*Notification)
	if !ok {
		return nil
	}

	diff := cmp.Diff(t.Status, og.Status)
	if diff == "" {
		return nil
	}

	clientObj, ok := original.(client.Object)
	if !ok {
		return nil
	}

	return client.MergeFrom(clientObj)
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
