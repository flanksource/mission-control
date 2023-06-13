package db

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func PersistConnectionFromCRD(obj *v1.Connection) error {
	dbObj := models.Connection{
		ID:          uuid.MustParse(string(obj.GetUID())),
		Name:        obj.Name,
		Type:        obj.Spec.Type,
		URL:         obj.Spec.URL.String(),
		Username:    obj.Spec.Username.String(),
		Password:    obj.Spec.Password.String(),
		Certificate: obj.Spec.Certificate.String(),
		Properties:  obj.Spec.Properties,
		InsecureTLS: obj.Spec.InsecureTLS,
		// Gorm.Save does not use defaults when inserting
		// and the timestamp used is zero time
		CreatedAt: time.Now(),
	}

	return Gorm.Save(&dbObj).Error
}

func DeleteConnection(id string) error {
	return Gorm.Table("connections").
		Delete(&models.Connection{}, "id = ?", id).
		Error
}
