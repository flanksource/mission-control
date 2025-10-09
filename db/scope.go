package db

import (
	"encoding/json"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// PersistScopeFromCRD saves a Scope CRD to the database
func PersistScopeFromCRD(ctx context.Context, obj *v1.Scope) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

	// Marshal targets to JSON for storage
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
		Source:      models.SourceCRD,
	}

	return ctx.DB().Save(&scope).Error
}

// DeleteScope soft deletes a Scope by ID
func DeleteScope(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Scope{}).
		Where("id = ?", id).
		Update("deleted_at", duty.Now()).Error
}

// DeleteStaleScope soft deletes old Scope resources with the same name/namespace
func DeleteStaleScope(ctx context.Context, newer *v1.Scope) error {
	return ctx.DB().Model(&models.Scope{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
