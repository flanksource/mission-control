package db

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func PersistNotificationFromCRD(obj *v1.Notification) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	dbObj := models.Notification{
		ID:         uid,
		Events:     obj.Spec.Events,
		Title:      obj.Spec.Title,
		Template:   obj.Spec.Template,
		Filter:     obj.Spec.Filter,
		Properties: obj.Spec.Properties,
	}

	if obj.Spec.Person != "" {
		if uid, err := uuid.Parse(obj.Spec.Person); err == nil {
			dbObj.PersonID = &uid
		} else {
			var person models.Person
			if err := Gorm.Where("email = ?", obj.Spec.Person).First(&person).Error; err != nil {
				return err
			}
			dbObj.PersonID = &person.ID
		}
	}

	if obj.Spec.Team != "" {
		if uid, err := uuid.Parse(obj.Spec.Team); err == nil {
			dbObj.TeamID = &uid
		} else {
			var person models.Team
			if err := Gorm.Where("name = ?", obj.Spec.Team).First(&person).Error; err != nil {
				return err
			}
			dbObj.TeamID = &person.ID
		}
	}

	if len(obj.Spec.CustomServices) != 0 {
		customServices, err := json.Marshal(obj.Spec.CustomServices)
		if err != nil {
			return err
		}

		dbObj.CustomServices = customServices
	}

	return Gorm.Save(&dbObj).Error
}

func DeleteNotification(id string) error {
	return Gorm.Delete(&models.Notification{}, "id = ?", id).Error
}
