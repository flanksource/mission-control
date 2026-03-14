package db

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/samber/lo"

	localAPI "github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func mergeConnectionModels(source, override models.Connection) models.Connection {
	source.ID = lo.CoalesceOrEmpty(override.ID, source.ID)
	source.Name = lo.CoalesceOrEmpty(override.Name, source.Name)
	source.Namespace = lo.CoalesceOrEmpty(override.Namespace, source.Namespace)
	source.Source = lo.CoalesceOrEmpty(override.Source, source.Source)
	source.Type = lo.CoalesceOrEmpty(override.Type, source.Type)
	source.URL = lo.CoalesceOrEmpty(override.URL, source.URL)
	source.Username = lo.CoalesceOrEmpty(override.Username, source.Username)
	source.Password = lo.CoalesceOrEmpty(override.Password, source.Password)
	source.Certificate = lo.CoalesceOrEmpty(override.Certificate, source.Certificate)
	source.InsecureTLS = override.InsecureTLS
	source.CreatedAt = lo.CoalesceOrEmpty(override.CreatedAt, source.CreatedAt)
	source.UpdatedAt = lo.CoalesceOrEmpty(override.UpdatedAt, source.UpdatedAt)
	source.CreatedBy = lo.CoalesceOrEmpty(override.CreatedBy, source.CreatedBy)
	source.Properties = collections.MergeMap(source.Properties, override.Properties)
	return source
}

func ConnectionFromCRD(obj *v1.Connection) models.Connection {
	dbObj := models.Connection{
		Name:        obj.Name,
		Namespace:   obj.Namespace,
		Type:        obj.Spec.Type,
		URL:         obj.Spec.URL.String(),
		Username:    obj.Spec.Username.String(),
		Password:    obj.Spec.Password.String(),
		Certificate: obj.Spec.Certificate.String(),
		Source:      models.SourceCRD,
	}

	if obj.GetUID() != "" {
		dbObj.ID = uuid.MustParse(string(obj.GetUID()))
	}

	connectionFromCRDSpec(obj, &dbObj)

	if len(obj.Spec.Properties) != 0 {
		dbObj.Properties = collections.MergeMap(obj.Spec.Properties, dbObj.Properties)
	}

	return dbObj
}

func PersistConnectionFromCRD(ctx context.Context, obj *v1.Connection) error {
	dbObj := ConnectionFromCRD(obj)
	dbObj.CreatedAt = time.Now()
	setConnectionRef(obj)

	if err := ctx.DB().Save(&dbObj).Error; err != nil {
		wrappedErr := fmt.Errorf("failed to persist connection %s/%s: %w", obj.Namespace, obj.Name, err)
		setConnectionPersistFailedStatus(obj, wrappedErr)
		persistConnectionStatus(ctx, obj)
		return wrappedErr
	}

	setConnectionReadyStatus(obj)
	return nil
}

func DeleteConnection(ctx context.Context, id string) error {
	var conn models.Connection
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Find(&conn).Error; err != nil {
		return fmt.Errorf("failed to find connection: %w", err)
	}

	if conn.ID == uuid.Nil {
		return nil
	}

	if err := ctx.DB().Model(&models.Connection{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
		wrappedErr := fmt.Errorf("failed to soft-delete connection: %w", err)
		persistConnectionDeleteFailedStatus(ctx, conn, wrappedErr)
		return wrappedErr
	}

	return nil
}

func DeleteStaleConnection(ctx context.Context, newer *v1.Connection) error {
	err := ctx.DB().Model(&models.Connection{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
	if err != nil {
		wrappedErr := fmt.Errorf("failed to soft-delete stale connections for %s/%s: %w", newer.Namespace, newer.Name, err)
		setConnectionDeleteFailedStatus(newer, wrappedErr)
		persistConnectionStatus(ctx, newer)
		return wrappedErr
	}

	return nil
}

func ListConnections(ctx context.Context) ([]models.Connection, error) {
	var c []models.Connection
	err := ctx.DB().Omit("password", "certificate").Where("deleted_at IS NULL").Find(&c).Error
	return c, err
}

func FindDefaultLLMProviderConnection(ctx context.Context) (*models.Connection, error) {
	if localAPI.DefaultLLMConnection == "" {
		return nil, api.Errorf(api.ENOTFOUND, "no default LLM connection configured. Use --llm-connection flag to specify one")
	}

	return connection.Get(ctx, localAPI.DefaultLLMConnection)
}
