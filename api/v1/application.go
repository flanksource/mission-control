package v1

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
)

type ViewRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ApplicationSection is a section rendered in the application view
type ApplicationSection struct {
	Title   string  `json:"title"`
	Icon    string  `json:"icon,omitempty"`
	ViewRef ViewRef `json:"viewRef"`
}

type ApplicationMapping struct {
	AccessReviews []types.ResourceSelector            `json:"accessReviews,omitempty"`
	Environments  map[string][]ApplicationEnvironment `json:"environments,omitempty"`

	// Datasources targets config items representing data sources (e.g. databases)
	// whose backups and restores should be monitored
	Datasources []types.ResourceSelector `json:"datasources,omitempty"`

	// Specifies which applications's users/groups and user-group membership are required
	Logins []types.ResourceSelector `json:"logins,omitempty"`

	// Defines mappings to automatically generate roles based on specified group associations
	Roles []ApplicationRoleMapping `json:"roles,omitempty"`
}

type ApplicationRoleMapping struct {
	types.ResourceSelector `json:",inline"`

	// Assign a name for the role
	Role string `json:"role"`
}

type ApplicationEnvironment struct {
	types.ResourceSelector `json:",inline"`

	// Purpose of the environment
	Purpose string `json:"purpose"`
}

type ApplicationSpec struct {
	// Description of the application
	Description string `json:"description,omitempty"`

	// Properties to be displayed in the application view
	Properties []api.Property `json:"properties,omitempty"`

	// Type of the application
	Type string `json:"type"`

	//+kubebuilder:validation:Optional
	Mapping ApplicationMapping `json:"mapping,omitempty"`

	Sections []ApplicationSection `json:"sections,omitempty"`
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

func (a *Application) GetID() uuid.UUID {
	uid, err := uuid.Parse(string(a.UID))
	if err != nil {
		return uuid.Nil
	}

	return uid
}

func (a *Application) AllSelectors() []types.ResourceSelector {
	var selectors []types.ResourceSelector
	selectors = append(selectors, a.Spec.Mapping.AccessReviews...)
	selectors = append(selectors, a.Spec.Mapping.Datasources...)
	selectors = append(selectors, a.Spec.Mapping.Logins...)
	for _, role := range a.Spec.Mapping.Roles {
		selectors = append(selectors, role.ResourceSelector)
	}

	for _, environment := range a.Spec.Mapping.Environments {
		for _, env := range environment {
			selectors = append(selectors, env.ResourceSelector)
		}
	}

	return selectors
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
