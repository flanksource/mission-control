package v1

import (
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func findSubject(ctx context.Context, table string, selector string, subjectType models.PermissionSubjectType) (string, models.PermissionSubjectType, error) {
	if uuid.Validate(selector) == nil {
		var id string
		err := ctx.DB().Select("id").Table(table).Where("id = ?", selector).Find(&id).Error
		return id, subjectType, err
	}

	// Otherwise look up by name or email
	var id string
	var err error
	if table == "people" {
		err = ctx.DB().Select("id").Table(table).Where("name = ? OR email = ?", selector, selector).Find(&id).Error
	} else {
		err = ctx.DB().Select("id").Table(table).Where("name = ?", selector).Find(&id).Error
	}
	return id, subjectType, err
}

func findNamespacedResource(ctx context.Context, table string, selector string, subjectType models.PermissionSubjectType) (string, models.PermissionSubjectType, error) {
	if uuid.Validate(selector) == nil {
		var id string
		err := ctx.DB().Select("id").Table(table).Where("id = ?", selector).Find(&id).Error
		return id, subjectType, err
	}

	// Parse namespace/name format
	splits := strings.Split(selector, "/")
	if len(splits) != 2 {
		return "", "", fmt.Errorf("%s is not a valid subject. Must be <namespace>/<name>", selector)
	}

	namespace, name := splits[0], splits[1]
	var id string
	err := ctx.DB().Select("id").Table(table).
		Where("namespace = ?", namespace).
		Where("name = ?", name).
		Find(&id).Error
	return id, subjectType, err
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
		return findSubject(ctx, "people", string(t.Person), models.PermissionSubjectTypePerson)
	}
	if t.Team != "" {
		return findSubject(ctx, "teams", string(t.Team), models.PermissionSubjectTypeTeam)
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

type PermissionObject dutyRBAC.Selectors

// GlobalObject checks if the object selector semantically maps to a global object
// and returns the corresponding global object if applicable.
// For example:
//
//	configs:
//		- name: '*'
//
// is interpreted as the object: catalog.
func (t *PermissionObject) GlobalObject() (string, bool) {
	switch {
	case t.isWildcardOnly(t.Playbooks, t.Configs, t.Components, t.Connections):
		return policy.ObjectPlaybooks, true
	case t.isWildcardOnly(t.Configs, t.Playbooks, t.Components, t.Connections):
		return policy.ObjectCatalog, true
	case t.isWildcardOnly(t.Components, t.Playbooks, t.Configs, t.Connections):
		return policy.ObjectTopology, true
	case t.isWildcardOnly(t.Connections, t.Playbooks, t.Configs, t.Components):
		return policy.ObjectConnection, true
	default:
		return "", false
	}
}

func (t *PermissionObject) isWildcardOnly(primary []types.ResourceSelector, others ...[]types.ResourceSelector) bool {
	for _, other := range others {
		if len(other) != 0 {
			return false
		}
	}

	return len(primary) == 1 && primary[0].Wildcard()
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
	Agents []string `json:"agents,omitempty"`

	// List of config/component tags a person is allowed to access to when RLS is enabled
	Tags map[string]string `json:"tags,omitempty"`
}

type PermissionStatus struct {
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
