package db

import (
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/lib/pq"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// PersistScopeBindingFromCRD saves a ScopeBinding CRD to the database
func PersistScopeBindingFromCRD(ctx context.Context, obj *v1.ScopeBinding) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

	if obj.Spec.Subjects.Empty() {
		return ctx.Oops().Errorf("subjects cannot be empty")
	}

	scopeBinding := models.ScopeBinding{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Persons:     pq.StringArray(obj.Spec.Subjects.Persons),
		Teams:       pq.StringArray(obj.Spec.Subjects.Teams),
		Scopes:      pq.StringArray(obj.Spec.Scopes),
		Source:      models.SourceCRD,
	}

	return ctx.DB().Save(&scopeBinding).Error
}

// DeleteScopeBinding soft deletes a ScopeBinding by ID
func DeleteScopeBinding(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.ScopeBinding{}).
		Where("id = ?", id).
		Update("deleted_at", duty.Now()).Error
}

// DeleteStaleScopeBinding soft deletes old ScopeBinding resources with the same name/namespace
func DeleteStaleScopeBinding(ctx context.Context, newer *v1.ScopeBinding) error {
	return ctx.DB().Model(&models.ScopeBinding{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
