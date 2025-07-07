package db

import (
	"encoding/json"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// PersistViewFromCRD persists a View CRD to the database
func PersistViewFromCRD(ctx context.Context, obj *v1.View) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	if err := obj.Spec.Validate(); err != nil {
		return err
	}

	specJSON, err := json.Marshal(obj.Spec)
	if err != nil {
		return err
	}

	view := models.View{
		ID:        uid,
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Spec:      specJSON,
		Source:    models.SourceCRD,
	}

	return ctx.DB().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"spec", "source"}), // only these values can be updated. (otherwise last_ran, error fields would reset)
	}).Create(&view).Error
}

// DeleteView soft deletes a View by setting deleted_at timestamp
func DeleteView(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.View{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleView soft deletes stale Views that match name and namespace
func DeleteStaleView(ctx context.Context, newer *v1.View) error {
	return ctx.DB().Model(&models.View{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}

// GetView retrieves a view by name and namespace
func GetView(ctx context.Context, namespace, name string) (*v1.View, error) {
	var view models.View
	if err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).First(&view).Error; err != nil {
		return nil, err
	}

	var spec v1.ViewSpec
	if err := json.Unmarshal(view.Spec, &spec); err != nil {
		return nil, err
	}

	viewCR := &v1.View{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(view.ID.String()),
			Name:      view.Name,
			Namespace: view.Namespace,
		},
		Spec: spec,
	}

	if view.LastRan != nil {
		viewCR.Status.LastRan = &metav1.Time{Time: *view.LastRan}
	}

	return viewCR, nil
}

// GetAllViews fetches all views from the database
func GetAllViews(ctx context.Context) ([]models.View, error) {
	var views []models.View
	err := ctx.DB().Where("deleted_at IS NULL").Find(&views).Error
	return views, err
}
