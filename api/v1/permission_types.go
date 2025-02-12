package v1

import (
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"

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

// Subject of the permission.
// Can be in various formats depending on the table
// - id of a resource
// - name or email for person
// - <namespace>/<name> of a notification
type PermissionSubjectSelector string

func (t PermissionSubjectSelector) Find(ctx context.Context, table string) (string, models.PermissionSubjectType, error) {
	if uuid.Validate(string(t)) == nil {
		return string(t), "", nil
	}

	switch table {
	case "people":
		var id string
		if uuid.Validate(string(t)) == nil {
			err := ctx.DB().Select("id").Model(table).Where("id = ?", string(t)).Find(&id).Error
			return id, models.PermissionSubjectTypePerson, err
		} else {
			err := ctx.DB().Select("id").Model(table).Where("name = ? OR email = ?", string(t), string(t)).Find(&id).Error
			return id, models.PermissionSubjectTypePerson, err
		}

	case "teams":
		var id string
		if uuid.Validate(string(t)) == nil {
			err := ctx.DB().Select("id").Model(table).Where("id = ?", string(t)).Find(&id).Error
			return id, models.PermissionSubjectTypeTeam, err
		} else {
			err := ctx.DB().Select("id").Table(table).Where("name = ?", string(t)).Find(&id).Error
			return id, models.PermissionSubjectTypeTeam, err
		}

	case "notifications":
		if uuid.Validate(string(t)) == nil {
			var id string
			err := ctx.DB().Select("id").Table(table).Where("id = ?", string(t)).Find(&id).Error
			return id, models.PermissionSubjectTypeNotification, err
		}

		splits := strings.Split(string(t), "/")
		if len(splits) != 2 {
			return "", "", fmt.Errorf("%s is not a valid notification subject. must be <namespace>/<name>", t)
		}

		namespace, name := splits[0], splits[1]
		var id string
		err := ctx.DB().Select("id").Table(table).
			Where("namespace = ?", namespace).
			Where("name = ?", name).
			Find(&id).Error
		return id, models.PermissionSubjectTypeNotification, err
	}

	return "", "", nil
}

type PermissionSubject struct {
	// ID or email of the person
	Person PermissionSubjectSelector `json:"person,omitempty"`

	// Team is the team name
	Team PermissionSubjectSelector `json:"team,omitempty"`

	// Notification <namespace>/<name> selector
	Notification PermissionSubjectSelector `json:"notification,omitempty"`

	// Group is the group name
	Group string `json:"group,omitempty"`
}

func (t *PermissionSubject) Validate() error {
	if t.Person == "" && t.Team == "" && t.Notification == "" && t.Group == "" {
		return errors.New("subject is empty: one of person, team, notification or a group is required")
	}

	return nil
}

func (t *PermissionSubject) Populate(ctx context.Context) (string, models.PermissionSubjectType, error) {
	if err := t.Validate(); err != nil {
		return "", "", err
	}

	if t.Person != "" {
		return t.Person.Find(ctx, "people")
	}
	if t.Team != "" {
		return t.Team.Find(ctx, "teams")
	}
	if t.Notification != "" {
		return t.Notification.Find(ctx, "notifications")
	}
	if t.Group != "" {
		return string(t.Group), models.PermissionSubjectTypeGroup, nil
	}

	return "", "", errors.New("subject not found")
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
	if len(t.Playbooks) == 1 && len(t.Configs) == 0 && len(t.Components) == 0 && t.Playbooks[0].Wildcard() {
		return policy.ObjectPlaybooks, true
	}

	if len(t.Configs) == 1 && len(t.Playbooks) == 0 && len(t.Components) == 0 && t.Configs[0].Wildcard() {
		return policy.ObjectCatalog, true
	}

	if len(t.Components) == 1 && len(t.Playbooks) == 0 && len(t.Configs) == 0 && t.Components[0].Wildcard() {
		return policy.ObjectTopology, true
	}

	return "", false
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
