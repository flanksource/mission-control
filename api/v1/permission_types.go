package v1

import (
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
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

// Subject of the permission.
// Can be
// - a permission group name
// - id of a resource
// - <namespace>/<name> of a resource
type PermissionSubjectSelector string

func (t PermissionSubjectSelector) Find(ctx context.Context, table string) (string, models.PermissionSubjectType, error) {
	if uuid.Validate(string(t)) == nil {
		return string(t), "", nil
	}

	switch table {
	case "people":
		var id string
		err := ctx.DB().Select("id").Table(table).Where("name = ? OR email = ?", string(t), string(t)).Find(&id).Error
		return id, models.PermissionSubjectTypePerson, err

	case "teams":
		var id string
		err := ctx.DB().Select("id").Table(table).Where("name = ?", string(t)).Find(&id).Error
		return id, models.PermissionSubjectTypeTeam, err

	case "notifications":
		splits := strings.Split(string(t), "/")
		switch len(splits) {
		case 1:
			return string(t), models.PermissionSubjectTypeGroup, nil // assume it's the group name

		case 2:
			namespace, name := splits[0], splits[1]
			var id string
			err := ctx.DB().Select("id").Table(table).
				Where("namespace = ?", namespace).
				Where("name = ?", name).
				Find(&id).Error
			return id, models.PermissionSubjectTypeNotification, err
		}

	default:
		return "", "", fmt.Errorf("unknown table: %v", table)
	}

	return "", "", nil
}

type PermissionSubject struct {
	Person       PermissionSubjectSelector `json:"person,omitempty"`
	Team         PermissionSubjectSelector `json:"team,omitempty"`
	Notification PermissionSubjectSelector `json:"notification,omitempty"`
}

func (t *PermissionSubject) Validate() error {
	if t.Person == "" && t.Team == "" && t.Notification == "" {
		return errors.New("subject is empty: one of permission, team or notification is required")
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

	return "", "", errors.New("subject not found")
}

type PermissionObject struct {
	Playbooks  []types.ResourceSelector `json:"playbooks,omitempty"`
	Configs    []types.ResourceSelector `json:"configs,omitempty"`
	Components []types.ResourceSelector `json:"components,omitempty"`
}

func (t PermissionObject) RequiredMatchCount() int {
	var count int
	if len(t.Playbooks) > 0 {
		count++
	}
	if len(t.Configs) > 0 {
		count++
	}
	if len(t.Components) > 0 {
		count++
	}

	return count
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
