// Package v1 contains API Schema definitions for the mission-control v1 API group
// +kubebuilder:object:generate=true
// +groupName=mission-control.flanksource.com
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// groupVersion is group version used to register these objects
	groupVersion = schema.GroupVersion{Group: "mission-control.flanksource.com", Version: "v1"}

	// schemeBuilder is used to add go types to the GroupVersionKind scheme
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = schemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(groupVersion,
		&Application{}, &ApplicationList{},
		&Connection{}, &ConnectionList{},
		&IncidentRule{}, &IncidentRuleList{},
		&Notification{}, &NotificationList{},
		&NotificationSilence{}, &NotificationSilenceList{},
		&Permission{}, &PermissionList{},
		&PermissionGroup{}, &PermissionGroupList{},
		&Playbook{}, &PlaybookList{},
		&Plugin{}, &PluginList{},
		&Scope{}, &ScopeList{},
		&Team{}, &TeamList{},
		&View{}, &ViewList{},
	)

	metav1.AddToGroupVersion(scheme, groupVersion)
	return nil
}
