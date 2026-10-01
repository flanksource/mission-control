package db

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// validity returns the error and reason to store for an object, from its validation error.
// Errors other than validation errors are returned, and nothing is stored.
func validity(err error) (message, reason *string, _ error) {
	if err == nil {
		return nil, nil, nil
	} else if !adapter.IsValidationError(err) {
		return nil, nil, err
	}
	return lo.ToPtr(err.Error()), lo.ToPtr(adapter.InvalidReason(err)), nil
}

// notReadyRetry is when an object that isn't in effect is validated again. Changes to the Scopes, Roles and
// RoleBindings it references reconcile it right away; this catches the rest, e.g. an agent registered again.
const notReadyRetry = 5 * time.Minute

// notReady reports a stored object that isn't in effect, so its status is Ready=False with the reason.
func notReady(reason, message *string) error {
	if message == nil {
		return nil
	}
	return &kopper.NotReadyError{Reason: lo.FromPtr(reason), Message: lo.FromPtr(message), RequeueAfter: notReadyRetry}
}

// PersistRoleFromCRD stores a Role CRD. See PersistRole.
func PersistRoleFromCRD(ctx context.Context, obj *v1.Role) error {
	return PersistRole(ctx, obj, models.SourceCRD, nil)
}

// PersistRole stores the role as written, with why it isn't in effect when it's invalid.
// An invalid role is stored too: it applies none of its rules until it becomes valid.
func PersistRole(ctx context.Context, obj *v1.Role, source string, createdBy *uuid.UUID) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	rules, err := json.Marshal(obj.Spec.Rules)
	if err != nil {
		return err
	}

	role := models.Role{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Source:      source,
		Rules:       rules,
		CreatedBy:   createdBy,
	}

	_, validationErr := adapter.ValidateRole(ctx, nil, role)
	if role.Error, role.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	// Returned unwrapped so kopper can detect unique constraint violations and delete the stale role
	if err := ctx.DB().Save(&role).Error; err != nil {
		return err
	}

	return notReady(role.ErrorReason, role.Error)
}

func DeleteRole(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Role{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleRole replaces an older role of the same name with the newer one.
func DeleteStaleRole(ctx context.Context, newer *v1.Role) error {
	if err := ctx.DB().Model(&models.Role{}).
		Where("namespace = ? AND name = ?", newer.Namespace, newer.Name).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error; err != nil {
		return err
	}

	return PersistRoleFromCRD(ctx, newer)
}

// PersistRoleBindingFromCRD stores a RoleBinding CRD. See PersistRoleBinding.
func PersistRoleBindingFromCRD(ctx context.Context, obj *v1.RoleBinding) error {
	return PersistRoleBinding(ctx, obj, models.SourceCRD, nil)
}

// PersistRoleBinding stores the binding as written, with why it isn't in effect when it's invalid:
// its role doesn't exist or is invalid, or a constraint doesn't fit the role.
func PersistRoleBinding(ctx context.Context, obj *v1.RoleBinding, source string, createdBy *uuid.UUID) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	subjects, err := json.Marshal(obj.Spec.Subjects)
	if err != nil {
		return err
	}

	constraints, err := json.Marshal(lo.CoalesceSliceOrEmpty(obj.Spec.Constraints))
	if err != nil {
		return err
	}

	binding := models.RoleBinding{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Source:      source,
		Role:        obj.Spec.Role,
		Constraints: constraints,
		Subjects:    subjects,
		CreatedBy:   createdBy,
	}

	validationErr := adapter.ValidateBinding(ctx, nil, binding)
	if binding.Error, binding.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	if err := ctx.DB().Save(&binding).Error; err != nil {
		return err
	}

	return notReady(binding.ErrorReason, binding.Error)
}

func DeleteRoleBinding(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.RoleBinding{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleRoleBinding replaces an older binding of the same name with the newer one.
func DeleteStaleRoleBinding(ctx context.Context, newer *v1.RoleBinding) error {
	if err := ctx.DB().Model(&models.RoleBinding{}).
		Where("namespace = ? AND name = ?", newer.Namespace, newer.Name).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error; err != nil {
		return err
	}

	return PersistRoleBindingFromCRD(ctx, newer)
}
