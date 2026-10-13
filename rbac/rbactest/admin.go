package rbactest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

// MakeAdmins writes the shipped objects and adds the people to the shipped admins team, bound to the admin Role.
// That's how a person is an admin: built-in roles grant nothing.
func MakeAdmins(ctx context.Context, personIDs ...uuid.UUID) error {
	if err := shipped.Sync(ctx); err != nil {
		return err
	}
	for _, id := range personIDs {
		if err := shipped.AddToAdmins(ctx, id); err != nil {
			return err
		}
	}
	return dutyRBAC.ReloadPolicy()
}

// BindWholeType creates, in the namespace, a Scope of every resource of each type, a Role allowing the actions on
// them, and a RoleBinding of the people (by email) to the Role.
func BindWholeType(ctx context.Context, namespace, name string, emails, resources, actions []string) error {
	var targets []v1.ScopeTarget
	for _, resource := range resources {
		var target v1.ScopeTarget
		if err := json.Unmarshal(fmt.Appendf(nil, `{%q: {"name": "*"}}`, resource), &target); err != nil {
			return err
		}
		targets = append(targets, target)
	}

	meta := func() metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())}
	}
	if err := db.PersistScopeFromCRD(ctx, &v1.Scope{ObjectMeta: meta(), Spec: v1.ScopeSpec{Targets: targets}}); err != nil {
		return err
	}

	var rules []v1.RoleRule
	for _, action := range actions {
		rules = append(rules, v1.RoleRule{Name: strings.ReplaceAll(action, ":", "-"), Action: action, Resource: &v1.ScopeReference{ScopeRef: name}})
	}
	if err := db.PersistRoleFromCRD(ctx, &v1.Role{ObjectMeta: meta(), Spec: v1.RoleSpec{Rules: rules}}); err != nil {
		return err
	}

	subjects := v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: emails}}
	if err := db.PersistRoleBindingFromCRD(ctx, &v1.RoleBinding{ObjectMeta: meta(), Spec: v1.RoleBindingSpec{Role: name, Subjects: subjects}}); err != nil {
		return err
	}
	return dutyRBAC.ReloadPolicy()
}

// DeleteNamespace deletes the Scopes, Roles and RoleBindings of the namespace, and reloads the policy.
func DeleteNamespace(ctx context.Context, namespace string) error {
	for _, model := range []any{&models.RoleBinding{}, &models.Role{}, &models.Scope{}} {
		if err := ctx.DB().Where("namespace = ?", namespace).Delete(model).Error; err != nil {
			return err
		}
	}
	return dutyRBAC.ReloadPolicy()
}
