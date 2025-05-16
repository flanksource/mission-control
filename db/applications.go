package db

import (
	"encoding/json"

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

		return nil
	})
}
