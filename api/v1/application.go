package v1

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
)

type ApplicationMapping struct {
	AccessReviews []types.ResourceSelector            `json:"accessReviews,omitempty"`
	Datasources   []types.ResourceSelector            `json:"datasources,omitempty"`
	Environments  map[string][]types.ResourceSelector `json:"environments,omitempty"`

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

type ApplicationSpec struct {
	// Description of the application
	Description string `json:"description,omitempty"`

	// Type of the application
	Type string `json:"type"`

	// Schedule on which the application scrapes the data
	Schedule string `json:"schedule"`

	Mapping ApplicationMapping `json:"mapping"`

	Properties []Property `json:"properties,omitempty"`
}

type Property struct {
	Label   string       `json:"label,omitempty"`
	Name    string       `json:"name,omitempty"`
	Tooltip string       `json:"tooltip,omitempty"`
	Icon    string       `json:"icon,omitempty"`
	Text    string       `json:"text,omitempty"`
	Order   int          `json:"order,omitempty"`
	Type    string       `json:"type,omitempty"`
	Color   string       `json:"color,omitempty"`
	Value   *int64       `json:"value,omitempty"`
	Links   []types.Link `json:"links,omitempty"`

	// e.g. milliseconds, bytes, millicores, epoch etc.
	Unit string `json:"unit,omitempty"`
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
