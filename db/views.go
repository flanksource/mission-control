package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm/clause"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func setViewStatusCondition(obj *v1.View, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	obj.Status.ObservedGeneration = obj.Generation
	k8smeta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
		LastTransitionTime: now,
	})
}

func setViewValidationFailedStatus(obj *v1.View, err error) {
	if err == nil {
		return
	}
	setViewStatusCondition(obj, metav1.ConditionFalse, v1.ReadyReasonValidationFailed, err.Error())
}

func setViewPersistFailedStatus(obj *v1.View, err error) {
	if err == nil {
		return
	}
	setViewStatusCondition(obj, metav1.ConditionFalse, v1.ReadyReasonPersistFailed, err.Error())
}

func setViewDeleteFailedStatus(obj *v1.View, err error) {
	if err == nil {
		return
	}
	setViewStatusCondition(obj, metav1.ConditionFalse, v1.ReadyReasonDeleteFailed, err.Error())
}

func setViewReadyStatus(obj *v1.View) {
	setViewStatusCondition(obj, metav1.ConditionTrue, v1.ReadyReasonSynced, "View is valid and persisted")
}

func persistViewStatus(ctx context.Context, obj *v1.View) {
	if obj == nil || obj.Namespace == "" || obj.Name == "" {
		return
	}

	k8s, err := ctx.LocalKubernetes()
	if err != nil {
		ctx.Tracef("failed to initialize kubernetes client for view status update %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	resourceClient, err := k8s.GetClientByGroupVersionKind(ctx, v1.GroupVersion.Group, v1.GroupVersion.Version, "View")
	if err != nil {
		ctx.Tracef("failed to load view resource client for status update %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	resource, err := resourceClient.Namespace(obj.Namespace).Get(ctx, obj.Name, metav1.GetOptions{})
	if err != nil {
		ctx.Tracef("failed to fetch view %s/%s while updating status: %v", obj.Namespace, obj.Name, err)
		return
	}

	var mergedStatus v1.ViewStatus
	existingStatusMap, found, err := unstructured.NestedMap(resource.Object, "status")
	if err != nil {
		ctx.Tracef("failed to read existing view status for %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	if found {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existingStatusMap, &mergedStatus); err != nil {
			ctx.Tracef("failed to decode existing view status for %s/%s: %v", obj.Namespace, obj.Name, err)
			return
		}
	}

	if obj.Status.ObservedGeneration != 0 {
		mergedStatus.ObservedGeneration = obj.Status.ObservedGeneration
	}

	for _, c := range obj.Status.Conditions {
		k8smeta.SetStatusCondition(&mergedStatus.Conditions, c)
	}

	if obj.Status.LastRan != nil {
		mergedStatus.LastRan = obj.Status.LastRan
	}

	statusMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&mergedStatus)
	if err != nil {
		ctx.Tracef("failed to convert view status for %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	if err := unstructured.SetNestedMap(resource.Object, statusMap, "status"); err != nil {
		ctx.Tracef("failed to set view status for %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	if _, err := resourceClient.Namespace(obj.Namespace).UpdateStatus(ctx, resource, metav1.UpdateOptions{}); err != nil {
		ctx.Tracef("failed to persist view status for %s/%s: %v", obj.Namespace, obj.Name, err)
	}
}

func persistViewDeleteFailedStatus(ctx context.Context, view models.View, err error) {
	if err == nil {
		return
	}

	obj := &v1.View{
		ObjectMeta: metav1.ObjectMeta{
			Name:      view.Name,
			Namespace: view.Namespace,
		},
	}
	setViewDeleteFailedStatus(obj, err)
	persistViewStatus(ctx, obj)
}

// PersistViewFromCRD persists a View CRD to the database
func PersistViewFromCRD(ctx context.Context, obj *v1.View) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		setViewPersistFailedStatus(obj, fmt.Errorf("failed to parse uid: %w", err))
		return nil
	}

	if err := obj.Spec.Validate(); err != nil {
		setViewValidationFailedStatus(obj, err)
		return nil
	}

	specJSON, err := json.Marshal(obj.Spec)
	if err != nil {
		setViewPersistFailedStatus(obj, fmt.Errorf("failed to marshal view spec: %w", err))
		return nil
	}

	view := models.View{
		ID:        uid,
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Spec:      specJSON,
		Source:    models.SourceCRD,
		Labels:    obj.Labels,
	}

	if err := ctx.DB().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"spec", "source", "labels"}), // only these values can be updated. (otherwise last_ran, error fields would reset)
	}).Create(&view).Error; err != nil {
		wrappedErr := fmt.Errorf("failed to persist view %s/%s: %w", obj.Namespace, obj.Name, err)
		setViewPersistFailedStatus(obj, wrappedErr)
		persistViewStatus(ctx, obj)
		return wrappedErr
	}

	setViewReadyStatus(obj)
	return nil
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
		wrappedErr := fmt.Errorf("failed to drop generated table %s: %w", generatedTableName, err)
		persistViewDeleteFailedStatus(ctx, view, wrappedErr)
		return wrappedErr
	}

	if err := ctx.DB().Where("view_id = ?", id).Delete(&models.ViewPanel{}).Error; err != nil {
		wrappedErr := fmt.Errorf("failed to delete view panels: %w", err)
		persistViewDeleteFailedStatus(ctx, view, wrappedErr)
		return wrappedErr
	}

	if err := ctx.DB().Model(&models.View{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
		wrappedErr := fmt.Errorf("failed to soft-delete view: %w", err)
		persistViewDeleteFailedStatus(ctx, view, wrappedErr)
		return wrappedErr
	}

	return nil
}

// DeleteStaleView soft deletes stale Views that match name and namespace
func DeleteStaleView(ctx context.Context, newer *v1.View) error {
	err := ctx.DB().Model(&models.View{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
	if err != nil {
		wrappedErr := fmt.Errorf("failed to soft-delete stale views for %s/%s: %w", newer.Namespace, newer.Name, err)
		setViewDeleteFailedStatus(newer, wrappedErr)
		persistViewStatus(ctx, newer)
		return wrappedErr
	}

	return nil
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
			Labels:    view.Labels,
		},
		Spec: spec,
	}

	return viewCR, nil
}

type ViewDisplayPlugins struct {
	ConfigTab dutyTypes.ResourceSelector `json:"configTab"`
	Variables dutyTypes.JSONStringMap    `json:"variables,omitempty"`
}

func GetDisplayPlugins(ctx context.Context, id string) ([]ViewDisplayPlugins, error) {
	var p string
	if err := ctx.DB().Model(&models.View{}).Select("spec->'display'->>'plugins'").Where("id = ?", id).Scan(&p).Error; err != nil {
		return nil, err
	}

	if p == "" || p == "null" {
		return []ViewDisplayPlugins{}, nil
	}

	var plugins []ViewDisplayPlugins
	if err := json.Unmarshal([]byte(p), &plugins); err != nil {
		return nil, fmt.Errorf("failed to unmarshal display plugins: %w", err)
	}

	return plugins, nil
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
