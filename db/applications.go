package db

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

func GetAllApplications(ctx context.Context) ([]models.Application, error) {
	var apps []models.Application
	if err := ctx.DB().Where("deleted_at IS NULL").Find(&apps).Error; err != nil {
		return nil, err
	}

	return apps, nil
}

func FindApplication(ctx context.Context, namespace, name string) (*models.Application, error) {
	var app models.Application
	if err := ctx.DB().Where("deleted_at IS NULL").Where("name = ? AND namespace = ?", name, namespace).Find(&app).Error; err != nil {
		return nil, err
	}

	if app.ID == uuid.Nil {
		return nil, nil
	}

	return &app, nil
}

func PersistApplicationFromCRD(ctx context.Context, obj *v1.Application) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	spec, err := json.Marshal(obj.Spec)
	if err != nil {
		return err
	}

	app := models.Application{
		ID:          uid,
		Name:        obj.Name,
		Namespace:   obj.Namespace,
		Description: obj.Spec.Description,
		Spec:        string(spec),
		Source:      models.SourceCRD,
	}

	return ctx.DB().Save(&app).Error
}

func DeleteApplication(ctx context.Context, id string) error {
	return ctx.Transaction(func(txCtx context.Context, span trace.Span) error {
		if err := txCtx.DB().Model(&models.Application{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		if err := txCtx.DB().Model(&models.ConfigScraper{}).Where("application_id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		// Delete custom roles
		if err := txCtx.DB().Model(&models.ExternalRole{}).Where("application_id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		return nil
	})
}

func DeleteStaleApplication(ctx context.Context, newer *v1.Application) error {
	return ctx.Transaction(func(txCtx context.Context, span trace.Span) error {
		if err := ctx.DB().Model(&models.Application{}).
			Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
			Where("deleted_at IS NULL").
			Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		if err := txCtx.DB().Model(&models.ConfigScraper{}).Where("application_id = ?", newer.GetID()).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		// Delete custom roles
		if err := txCtx.DB().Model(&models.ExternalRole{}).Where("application_id = ?", newer.GetID()).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}

		return nil
	})
}

type ApplicationBackups struct {
	ID          uuid.UUID `json:"id"`
	ConfigID    uuid.UUID `json:"config_id"`
	Name        string    `json:"name"`
	ConfigType  string    `json:"type"`
	ConfigClass string    `json:"config_class"`
	ChangeType  string    `json:"change_type"`
	CreatedAt   time.Time `json:"created_at"`
	Size        string    `json:"size"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
}

func GetApplicationBackups(ctx context.Context, configIDs []uuid.UUID, changeTypes []string) ([]ApplicationBackups, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	var changes []ApplicationBackups
	selectColumns := []string{
		"config_changes.id",
		"config_changes.config_id",
		"config_items.name",
		"config_items.type",
		"config_items.config_class",
		"config_changes.change_type",
		"config_changes.created_at",
		"config_changes.source",
		"config_changes.details->>'status' AS status",
		"config_changes.details->>'size' AS size",
	}
	if err := ctx.DB().
		Model(&models.ConfigChange{}).
		Select(selectColumns).
		Joins("LEFT JOIN config_items ON config_items.id = config_changes.config_id").
		Where("config_changes.config_id IN (?) AND config_changes.change_type IN (?)", configIDs, changeTypes).
		Order("config_changes.created_at").
		Find(&changes).Error; err != nil {
		return nil, err
	}

	// filterChanges contains only BackupCompleted events.
	// It removes BackupStarted events if there's a corresponding BackupCompleted event
	var filteredChanges []ApplicationBackups
	for _, change := range changes {
		if strings.Contains(strings.ToLower(change.ChangeType), "completed") {
			// Pop the last element
			filteredChanges = filteredChanges[:len(filteredChanges)-1]
		}

		filteredChanges = append(filteredChanges, change)
	}

	slices.Reverse(filteredChanges)

	return filteredChanges, nil
}

type ApplicationRestore struct {
	ID          uuid.UUID `json:"id"`
	ConfigID    uuid.UUID `json:"config_id"`
	Name        string    `json:"name"`
	ConfigType  string    `json:"type"`
	ConfigClass string    `json:"config_class"`
	ChangeType  string    `json:"change_type"`
	CreatedAt   time.Time `json:"created_at"`
	Size        string    `json:"size"`
	Status      string    `json:"status"`
}

func GetApplicationRestores(ctx context.Context, configIDs []uuid.UUID, changeTypes []string) ([]ApplicationRestore, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	var changes []ApplicationRestore
	selectColumns := []string{
		"config_changes.id",
		"config_changes.config_id",
		"config_items.name",
		"config_items.type",
		"config_items.config_class",
		"config_changes.change_type",
		"config_changes.created_at",
		"config_changes.details->>'status' AS status",
		"config_changes.details->>'size' AS size",
	}
	if err := ctx.DB().
		Model(&models.ConfigChange{}).
		Select(selectColumns).
		Joins("LEFT JOIN config_items ON config_items.id = config_changes.config_id").
		Where("config_changes.config_id IN (?) AND config_changes.change_type IN (?)", configIDs, changeTypes).
		Order("config_changes.created_at").
		Find(&changes).Error; err != nil {
		return nil, err
	}

	return changes, nil
}
