package v1

import (
	"github.com/flanksource/duty/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ApplicationMapping struct {
	AccessReviews []types.ResourceSelector            `json:"accessReviews,omitempty"`
	Datasources   []types.ResourceSelector            `json:"datasources,omitempty"`
	Environments  map[string][]types.ResourceSelector `json:"environments,omitempty"`
	Login         []types.ResourceSelector            `json:"logins,omitempty"`
	Roles         []types.ResourceSelector            `json:"roles,omitempty"`
}

type ApplicationSpec struct {
	Mapping ApplicationMapping `json:"mapping"`
}

// ApplicationStatus defines the observed state of Application
type ApplicationStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Application is the Schema for the applications API
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ApplicationList contains a list of Application
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
