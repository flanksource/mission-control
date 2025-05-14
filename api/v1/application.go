package v1

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
)

type ApplicationMapping struct {
	AccessReviews []types.ResourceSelector            `json:"accessReviews,omitempty"`
	Datasources   []types.ResourceSelector            `json:"datasources,omitempty"`
	Environments  map[string][]types.ResourceSelector `json:"environments,omitempty"`
	Logins        []types.ResourceSelector            `json:"logins,omitempty"`
	Roles         []types.ResourceSelector            `json:"roles,omitempty"`
}

type ApplicationSpec struct {
	// Description of the application
	Description string `json:"description,omitempty"`

	// Schedule on which the application scrapes the data
	Schedule string `json:"schedule"`

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

func ApplicationFromModel(app models.Application) (*Application, error) {
	var spec ApplicationSpec
	if err := json.Unmarshal([]byte(app.Spec), &spec); err != nil {
		return nil, err
	}

	application := Application{
		ObjectMeta: metav1.ObjectMeta{
			UID:               k8sTypes.UID(app.ID.String()),
			Namespace:         app.Namespace,
			Name:              app.Name,
			CreationTimestamp: metav1.NewTime(app.CreatedAt),
		},
		Spec: spec,
	}

	return &application, nil
}

// +kubebuilder:object:root=true
// ApplicationList contains a list of Application
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
