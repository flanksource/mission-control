package db

import (
	"encoding/json"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersistScopeFromCRD stores a Scope CRD. See PersistScope.
func PersistScopeFromCRD(ctx context.Context, obj *v1.Scope) error {
	return PersistScope(ctx, obj, models.SourceCRD, nil)
}

// PersistScope stores the Scope as written, with why it isn't in effect when it's invalid.
// Agents are stored as written, and resolved every time the Scope is validated.
// A Scope is never validated against the Roles and RoleBindings that reference it.
func PersistScope(ctx context.Context, obj *v1.Scope, source string, createdBy *uuid.UUID) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

	targetsJSON, err := json.Marshal(obj.Spec.Targets)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to marshal targets")
	}

	scope := models.Scope{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Targets:     types.JSON(targetsJSON),
		Source:      source,
		CreatedBy:   createdBy,
	}

	_, validationErr := adapter.ValidateScope(ctx, nil, obj.Spec.Targets)
	if scope.Error, scope.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	// Returned unwrapped so kopper can detect unique constraint violations and delete the stale scope
	if err := ctx.DB().Save(&scope).Error; err != nil {
		return err
	}

	return notReady(scope.ErrorReason, scope.Error)
}

// DeleteScope soft deletes a Scope by ID. Roles and RoleBindings that reference it stop granting until they're updated.
func DeleteScope(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Scope{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleScope replaces an older Scope of the same name with the newer one.
func DeleteStaleScope(ctx context.Context, newer *v1.Scope) error {
	if err := ctx.DB().Model(&models.Scope{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error; err != nil {
		return err
	}

	return PersistScopeFromCRD(ctx, newer)
}
