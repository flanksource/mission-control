package db

import (
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/kopper"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// ReasonDeprecated is the reason of the Ready condition of every Permission and PermissionGroup.
const ReasonDeprecated = "Deprecated"

// PersistPermissionFromCRD stores nothing: Permissions are deprecated and grant nothing.
// It marks the Permission not ready, so it shows as deprecated.
func PersistPermissionFromCRD(ctx context.Context, obj *v1.Permission) error {
	return kopper.NotReady(ReasonDeprecated, "Permission is deprecated and grants nothing: use Scopes, Roles and RoleBindings instead")
}

// PersistPermissionGroupFromCRD stores nothing: PermissionGroups are deprecated and grant nothing.
// It marks the PermissionGroup not ready, so it shows as deprecated.
func PersistPermissionGroupFromCRD(ctx context.Context, obj *v1.PermissionGroup) error {
	return kopper.NotReady(ReasonDeprecated, "PermissionGroup is deprecated and grants nothing: use RoleBindings instead")
}

func DeletePermission(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Permission{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func DeletePermissionGroup(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.PermissionGroup{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}
