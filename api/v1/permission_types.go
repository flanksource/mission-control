package v1

import (
	"errors"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
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

func findUser(ctx context.Context, selector string) (string, models.PermissionSubjectType, error) {
	query := ctx.DB().Select("id").Table("people").Where("deleted_at IS NULL")

	if uuid.Validate(selector) == nil {
		var id string
		if err := query.Where("id = ?", selector).Find(&id).Error; err != nil {
			return "", "", ctx.Oops().Wrap(err)
		} else if id == "" {
			return "", "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %q not found", models.PermissionSubjectTypePerson, selector)
		}
		return id, models.PermissionSubjectTypePerson, nil
	}

	var id string
	if err := query.Where("email = ?", selector).Find(&id).Error; err != nil {
		return "", "", ctx.Oops().Wrap(err)
	} else if id == "" {
		return "", "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %q not found", models.PermissionSubjectTypePerson, selector)
	}

	return id, models.PermissionSubjectTypePerson, nil
}

func findTeam(ctx context.Context, selector string) (string, models.PermissionSubjectType, error) {
	query := ctx.DB().Select("id").Table("teams").Where("deleted_at IS NULL")

	if uuid.Validate(selector) == nil {
		var id string
		if err := query.Where("id = ?", selector).Find(&id).Error; err != nil {
			return "", "", ctx.Oops().Wrap(err)
		} else if id == "" {
			return "", "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %q not found", models.PermissionSubjectTypeTeam, selector)
		}
		return id, models.PermissionSubjectTypeTeam, nil
	}

	var id string
	if err := query.Where("name = ?", selector).Find(&id).Error; err != nil {
		return "", "", ctx.Oops().Wrap(err)
	} else if id == "" {
		return "", "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %q not found", models.PermissionSubjectTypeTeam, selector)
	}

	return id, models.PermissionSubjectTypeTeam, nil
}

func findNamespacedResource(ctx context.Context, table string, selector string, subjectType models.PermissionSubjectType) (string, models.PermissionSubjectType, error) {
	query := ctx.DB().Select("id").Table(table).Where("deleted_at IS NULL")

	if uuid.Validate(selector) == nil {
		var id string
		if err := query.Where("id = ?", selector).Find(&id).Error; err != nil {
			return "", "", ctx.Oops().Wrap(err)
		} else if id == "" {
			return "", "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %q not found", subjectType, selector)
		}
		return id, subjectType, nil
	}

	// Parse namespace/name format
	splits := strings.Split(selector, "/")
	if len(splits) != 2 {
		return "", "", dutyAPI.Errorf(dutyAPI.EINVALID, "%s is not a valid subject. Must be <namespace>/<name>", selector)
	}

	namespace, name := strings.TrimSpace(splits[0]), strings.TrimSpace(splits[1])
	if namespace == "" || name == "" {
		return "", "", dutyAPI.Errorf(dutyAPI.EINVALID, "%s is not a valid subject. Must be <namespace>/<name>", selector)
	}
	var id string
	err := query.
		Where("namespace = ?", namespace).
		Where("name = ?", name).
		Find(&id).Error
	if err != nil {
		return "", "", ctx.Oops().Wrap(err)
	} else if id == "" {
		return "", "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %q not found", subjectType, selector)
	}
	return id, subjectType, nil
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

	// Canary <namespace>/<name> selector
	Canary string `json:"canary,omitempty"`

	// Scraper <namespace>/<name> selector
	Scraper string `json:"scraper,omitempty"`

	// Topology <namespace>/<name> selector
	Topology string `json:"topology,omitempty"`
}

func (t *PermissionSubject) Empty() bool {
	return t.Person == "" && t.Team == "" && t.Notification == "" && t.Group == "" && t.Playbook == "" &&
		t.Canary == "" && t.Scraper == "" && t.Topology == ""
}

func (t *PermissionSubject) Populate(ctx context.Context) (string, models.PermissionSubjectType, error) {
	if t.Empty() {
		return "", "", errors.New("permission subject not specified")
	}

	if t.Group != "" {
		return string(t.Group), models.PermissionSubjectTypeGroup, nil
	}

	if t.Person != "" {
		return findUser(ctx, string(t.Person))
	}

	if t.Team != "" {
		return findTeam(ctx, string(t.Team))
	}

	if t.Notification != "" {
		return findNamespacedResource(ctx, "notifications", string(t.Notification), models.PermissionSubjectTypeNotification)
	}

	if t.Playbook != "" {
		return findNamespacedResource(ctx, "playbooks", string(t.Playbook), models.PermissionSubjectTypePlaybook)
	}

	if t.Canary != "" {
		return findNamespacedResource(ctx, "canaries", string(t.Canary), models.PermissionSubjectTypeCanary)
	}

	if t.Scraper != "" {
		return findNamespacedResource(ctx, "scrapers", string(t.Scraper), models.PermissionSubjectTypeScraper)
	}

	if t.Topology != "" {
		return findNamespacedResource(ctx, "topologies", string(t.Topology), models.PermissionSubjectTypeTopology)
	}

	return "", "", errors.New("permission subject not specified")
}

type PermissionGlobalObject string

const (
	PermissionGlobalObjectCanaries        PermissionGlobalObject = "canaries"
	PermissionGlobalObjectCatalog         PermissionGlobalObject = "catalog"
	PermissionGlobalObjectTopology        PermissionGlobalObject = "topology"
	PermissionGlobalObjectPlaybooks       PermissionGlobalObject = "playbooks"
	PermissionGlobalObjectConnection      PermissionGlobalObject = "connection"
	PermissionGlobalObjectAgentPush       PermissionGlobalObject = "agent-push"
	PermissionGlobalObjectKubernetesProxy PermissionGlobalObject = "kubernetes-proxy"
	PermissionGlobalObjectNotification    PermissionGlobalObject = "notification"
	PermissionGlobalObjectRBAC            PermissionGlobalObject = "rbac"
	PermissionGlobalObjectArtifact        PermissionGlobalObject = "artifact"
	PermissionGlobalObjectMCP             PermissionGlobalObject = "mcp"
)

// +kubebuilder:object:generate=false
// +kubebuilder:validation:XValidation:rule="!(has(self.global) && (has(self.configs) || has(self.components) || has(self.playbooks) || has(self.connections) || has(self.views) || has(self.scopes)))",message="object.global cannot be combined with selectors or scopes"
type PermissionObject struct {
	dutyRBAC.Selectors `json:",inline"`

	// Global explicitly sets a global RBAC object (e.g. catalog, topology, mcp).
	// When set, no resource selectors or scopes may be specified.
	// +kubebuilder:validation:Enum=canaries;catalog;topology;playbooks;connection;agent-push;kubernetes-proxy;notification;rbac;artifact;mcp
	Global PermissionGlobalObject `json:"global,omitempty"`

	// Scopes that is expanded onto Selectors
	Scopes []dutyRBAC.NamespacedNameIDSelector `json:"scopes,omitempty"`
}

// GlobalObject checks if the object selector semantically maps to a global object
// and returns the corresponding global object if applicable.
// For example:
//
//	configs:
//		- name: '*'
//
// is interpreted as the object: catalog.
func (t *PermissionObject) GlobalObject() (string, bool) {
	if t.Global != "" {
		return string(t.Global), true
	}

	switch {
	case t.isWildcardOnly(t.Playbooks, t.Configs, t.Components, t.Connections) && len(t.Views) == 0:
		return policy.ObjectPlaybooks, true
	case t.isWildcardOnly(t.Configs, t.Playbooks, t.Components, t.Connections) && len(t.Views) == 0:
		return policy.ObjectCatalog, true
	case t.isWildcardOnly(t.Components, t.Playbooks, t.Configs, t.Connections) && len(t.Views) == 0:
		return policy.ObjectTopology, true
	case t.isWildcardOnly(t.Connections, t.Playbooks, t.Configs, t.Components) && len(t.Views) == 0:
		return policy.ObjectConnection, true
	case t.isViewWildcardOnly():
		return policy.ObjectViews, true
	default:
		return "", false
	}
}

func (t *PermissionObject) HasSelectors() bool {
	return len(t.Playbooks) > 0 || len(t.Configs) > 0 || len(t.Components) > 0 || len(t.Connections) > 0 || len(t.Views) > 0 || len(t.Scopes) > 0
}

func (t *PermissionObject) Validate() error {
	if t.Global == "" {
		return nil
	}

	if t.HasSelectors() {
		return fmt.Errorf("permission object.global cannot be combined with selectors or scopes")
	}

	switch t.Global {
	case PermissionGlobalObjectCanaries,
		PermissionGlobalObjectCatalog,
		PermissionGlobalObjectTopology,
		PermissionGlobalObjectPlaybooks,
		PermissionGlobalObjectConnection,
		PermissionGlobalObjectAgentPush,
		PermissionGlobalObjectKubernetesProxy,
		PermissionGlobalObjectNotification,
		PermissionGlobalObjectRBAC,
		PermissionGlobalObjectArtifact,
		PermissionGlobalObjectMCP:
		return nil
	}

	return fmt.Errorf("invalid permission object.global %q", t.Global)
}

func (t *PermissionObject) isWildcardOnly(primary []types.ResourceSelector, others ...[]types.ResourceSelector) bool {
	for _, other := range others {
		if len(other) != 0 {
			return false
		}
	}

	return len(primary) == 1 && primary[0].Wildcard()
}

// isViewWildcardOnly checks if the permission object has only a wildcard view selector
// and no other resource selectors
func (t *PermissionObject) isViewWildcardOnly() bool {
	// Check that all other selectors are empty
	if len(t.Configs) != 0 || len(t.Components) != 0 ||
		len(t.Playbooks) != 0 || len(t.Connections) != 0 {
		return false
	}

	// Check that we have exactly one view with wildcard name
	return len(t.Views) == 1 && t.Views[0].Name == "*"
}

// +kubebuilder:object:generate=true
type PermissionSpec struct {
	// Description provides a brief explanation of the permission.
	Description string `json:"description,omitempty"`

	//+kubebuilder:validation:MinItems=1
	// Actions specify the operation that the permission allows or denies.
	Actions []string `json:"actions"`

	// Subject defines the entity (e.g., user, group) to which the permission applies.
	Subject PermissionSubject `json:"subject"`

	// Object identifies the resource or object that the permission is associated with.
	Object PermissionObject `json:"object"`

	// Deny indicates whether the permission should explicitly deny the specified action.
	//
	// Default: false
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

func init() {
	SchemeBuilder.Register(&Permission{}, &PermissionList{})
}
