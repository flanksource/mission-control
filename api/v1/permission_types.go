package v1

import (
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// Permission is the Schema for the Mission Control Permission
type Permission struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   PermissionSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status PermissionStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*Permission)(nil)
var _ kopper.StatusConditioner = (*Permission)(nil)
var _ kopper.ObservedGenerationSetter = (*Permission)(nil)

func (t *Permission) SetObservedGeneration(generation int64) {
	t.Status.ObservedGeneration = generation
}

func (t *Permission) GetObservedGeneration() int64 {
	return t.Status.ObservedGeneration
}

func (t *Permission) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *Permission) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*Permission)
	if !ok {
		return nil
	}

	if cmp.Diff(t.Status, og.Status) == "" {
		return nil
	}

	clientObj, ok := original.(client.Object)
	if !ok {
		return nil
	}

	return client.MergeFrom(clientObj)
}

// PluginSubject is the casbin subject a plugin's checks are made as.
func PluginSubject(namespace, name string) string {
	return "plugin:" + namespace + "/" + name
}

type PermissionSubject struct {
	// Group is the group name
	Group string `json:"group,omitempty"`

	// ID or email of the person
	Person string `json:"person,omitempty"`

	// Team is the team name
	Team string `json:"team,omitempty"`

	// Notification <namespace>/<name> selector
	Notification string `json:"notification,omitempty"`

	// Playbook <namespace>/<name> selector
	Playbook string `json:"playbook,omitempty"`

	// Plugin <namespace>/<name> or /<name> selector
	Plugin string `json:"plugin,omitempty"`

	// Canary <namespace>/<name> selector
	Canary string `json:"canary,omitempty"`

	// Scraper <namespace>/<name> selector
	Scraper string `json:"scraper,omitempty"`

	// Topology <namespace>/<name> selector
	Topology string `json:"topology,omitempty"`
}

// +kubebuilder:object:generate=false
// +kubebuilder:validation:XValidation:rule="!(has(self.mcp) && self.mcp && (has(self.configs) || has(self.components) || has(self.playbooks) || has(self.connections) || has(self.views) || has(self.scopes)))",message="object.mcp cannot be combined with selectors or scopes"
type PermissionObject struct {
	dutyRBAC.Selectors `json:",inline"`

	// MCP sets the global MCP object permission.
	// When true, no resource selectors or scopes may be specified.
	MCP bool `json:"mcp,omitempty"`

	// Scopes that is expanded onto Selectors
	Scopes []dutyRBAC.NamespacedNameIDSelector `json:"scopes,omitempty"`
}

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="!has(self.deny) || self.deny != true",message="deny isn't supported: permissions only allow"
type PermissionSpec struct {
	// Description provides a brief explanation of the permission.
	Description string `json:"description,omitempty"`

	//+kubebuilder:validation:MinItems=1
	// Actions specify the operation that the permission allows.
	Actions []string `json:"actions"`

	// Subject defines the entity (e.g., user, group) to which the permission applies.
	Subject PermissionSubject `json:"subject"`

	// Object identifies the resource or object that the permission is associated with.
	Object PermissionObject `json:"object"`

	// Deny is refused: permissions only allow. It's kept so that a Permission setting it is rejected rather than
	// stored without it, which would allow what it was written to deny. false is accepted and ignored.
	Deny bool `json:"deny,omitempty"`

	// List of agent ids whose configs/components are accessible to a person when RLS is enabled
	// DEPRECATED: Use AccessScope CRD instead. This field is ignored.
	// +optional
	Agents []string `json:"agents,omitempty"`

	// List of config/component tags a person is allowed to access to when RLS is enabled
	// DEPRECATED: Use AccessScope CRD instead. This field is ignored.
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

type PermissionStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
//
// PermissionList contains a list of Permission
type PermissionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Permission `json:"items"`
}
