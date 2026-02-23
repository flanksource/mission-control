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

type ApplicationMapping struct {
	// AccessReviews selects config items whose config_analysis records (security findings, misconfigurations, etc.)
	// must be shown in the application's Findings panel.
	AccessReviews []types.ResourceSelector `json:"accessReviews,omitempty"`

	// Environments maps an environment name (e.g. "Production", "Staging") to one or
	// more selectors that identify the infrastructure config items belonging to that
	// environment
	Environments map[string][]ApplicationEnvironment `json:"environments,omitempty"`

	// Datasources selects config items that represent data sources (e.g. databases,
	// object stores) whose backup and restore activity should be tracked.
	//
	// The system queries config_changes on the matched config items for the following
	// change types and surfaces them in the application's Backups / Restores panels:
	//   Backups:  BackupCompleted, BackupEnqueued, BackupFailed, BackupRunning,
	//             BackupStarted, BackupSuccessful
	//   Restores: BackupRestored, RestoreCompleted
	Datasources []types.ResourceSelector `json:"datasources,omitempty"`

	// Logins selects config items whose config access and access logs must be tracked by this application.
	//
	// This field has two effects:
	//
	// 1. Read path — the matched config items' access control are read and displayed.
	//
	// 2. Sync path (Azure only) — if the scraper that originally ingested the matched
	//    config items is an Azure scraper, it is cloned into a new application-scoped
	//    scraper reconfigured to scrape only Entra AppRoleAssignments (scoped to the
	//    matched items), keeping role assignment data fresh.
	Logins []types.ResourceSelector `json:"logins,omitempty"`

	// Roles maps external config items (e.g. Entra groups, IAM roles) to a named role
	// within this application, so that group memberships from external systems are
	// surfaced as human-readable roles in the application's AccessControl view.
	Roles []ApplicationRoleMapping `json:"roles,omitempty"`
}

type ApplicationRoleMapping struct {
	types.ResourceSelector `json:",inline"`

	// Assign a name for the role
	Role string `json:"role"`
}

type ApplicationEnvironment struct {
	types.ResourceSelector `json:",inline"`

	// Purpose describes the role of this environment within the application deployment.
	// e.g. "primary", "backup", "dr" (disaster recovery)
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

	Sections []api.ViewSection `json:"sections,omitempty"`
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
