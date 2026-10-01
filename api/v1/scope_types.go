package v1

import (
	"github.com/flanksource/duty/types"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ScopeTarget selects resources of exactly one type.
//
// A selector only accepts the fields its resource type supports, and rejects the rest.
// name matches exactly, or matches any name when set to "*". namespace and id only match exactly.
// tagSelector, labelSelector and fieldSelector use Kubernetes label selector syntax.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="[has(self.config), has(self.component), has(self.check), has(self.playbook), has(self.canary), has(self.view), has(self.connection), has(self.global)].filter(x, x).size() == 1",message="exactly one of config, component, check, playbook, canary, view, connection, or global must be specified"
type ScopeTarget struct {
	// Config selector
	Config *types.ResourceSelector `json:"config,omitempty"`

	// Component selector
	Component *types.ResourceSelector `json:"component,omitempty"`

	// Check selector
	Check *types.ResourceSelector `json:"check,omitempty"`

	// Playbook selector
	Playbook *types.ResourceSelector `json:"playbook,omitempty"`

	// Canary selector
	Canary *types.ResourceSelector `json:"canary,omitempty"`

	// View selector
	View *types.ResourceSelector `json:"view,omitempty"`

	// Connection selector
	Connection *types.ResourceSelector `json:"connection,omitempty"`

	// Global selector - applies to all resource types (wildcard).
	// Only Permissions use it. Roles and RoleBindings can't reference a Scope with a global target.
	Global *types.ResourceSelector `json:"global,omitempty"`
}

// +kubebuilder:object:generate=true
type ScopeSpec struct {
	// Description provides a brief explanation of what this scope covers
	Description string `json:"description,omitempty"`

	// Targets select the resources of the Scope.
	// A Scope is the union of what its targets select: a resource belongs to the Scope
	// when a target of the resource's type matches it.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Targets []ScopeTarget `json:"targets"`
}

type ScopeStatus struct {
	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Scope's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
//
// Scope selects resources, of one or more types, for access control
type Scope struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   ScopeSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status ScopeStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*Scope)(nil)
var _ kopper.StatusConditioner = (*Scope)(nil)
var _ kopper.ObservedGenerationSetter = (*Scope)(nil)

func (t *Scope) SetObservedGeneration(generation int64) {
	t.Status.ObservedGeneration = generation
}

func (t *Scope) GetObservedGeneration() int64 {
	return t.Status.ObservedGeneration
}

func (t *Scope) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *Scope) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*Scope)
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

// +kubebuilder:object:root=true
//
// ScopeList contains a list of Scope
type ScopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Scope `json:"items"`
}
