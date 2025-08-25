package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm/clause"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
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

// DeleteView soft deletes a View by setting deleted_at timestamp and cleans up associated tables
func DeleteView(ctx context.Context, id string) error {
	var view models.View
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Find(&view).Error; err != nil {
		return fmt.Errorf("failed to find view: %w", err)
	}

	if view.ID == uuid.Nil {
		return nil
	}

	generatedTableName := view.GeneratedTableName()
	if err := ctx.DB().Exec("DROP TABLE IF EXISTS " + pq.QuoteIdentifier(generatedTableName)).Error; err != nil {
		return fmt.Errorf("failed to drop generated table %s: %w", generatedTableName, err)
	}

	if err := ctx.DB().Where("view_id = ?", id).Delete(&models.ViewPanel{}).Error; err != nil {
		return fmt.Errorf("failed to delete view panels: %w", err)
	}

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
	if err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).Find(&view).Error; err != nil {
		return nil, err
	} else if view.ID == uuid.Nil {
		return nil, nil
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

func InsertPanelResults(ctx context.Context, viewID uuid.UUID, panels []api.PanelResult, requestFingerprint string) error {
	results, err := json.Marshal(panels)
	if err != nil {
		return fmt.Errorf("failed to marshal panel results: %w", err)
	}

	// Delete existing panel results for this view and fingerprint
	if err := ctx.DB().Where("view_id = ? AND request_fingerprint = ?", viewID, requestFingerprint).Delete(&models.ViewPanel{}).Error; err != nil {
		return fmt.Errorf("failed to delete existing panel results: %w", err)
	}

	record := models.ViewPanel{
		ViewID:             viewID,
		RequestFingerprint: requestFingerprint,
		Results:            results,
	}

	if err := ctx.DB().Create(&record).Error; err != nil {
		return fmt.Errorf("failed to save panel results: %w", err)
	}

	return nil
}

// FindViewsForConfig returns all the views that match the given config's resource selectors
func FindViewsForConfig(ctx context.Context, config models.ConfigItem) ([]api.ViewListItem, error) {
	var views []models.View
	if err := ctx.DB().Model(&models.View{}).Where(`spec->'display'->'plugins' IS NOT NULL AND jsonb_array_length(spec->'display'->'plugins') > 0`).Where("deleted_at IS NULL").Find(&views).Error; err != nil {
		return nil, fmt.Errorf("error finding views with ui plugins: %w", err)
	}

	viewListItems := make([]api.ViewListItem, 0)
	for _, view := range views {
		var spec v1.ViewSpec
		if err := json.Unmarshal(view.Spec, &spec); err != nil {
			return nil, fmt.Errorf("error unmarshaling view[%s] spec: %w", view.ID, err)
		}

		var matches bool
		for _, uiPlugin := range spec.Display.Plugins {
			if uiPlugin.ConfigTab.Matches(config) {
				matches = true
				break
			}
		}

		if !matches {
			continue
		}

		viewListItems = append(viewListItems, api.ViewListItem{
			ID:        view.ID,
			Name:      view.Name,
			Namespace: view.Namespace,
			Title:     spec.Display.Title,
			Icon:      spec.Display.Icon,
		})
	}

	return viewListItems, nil
}
