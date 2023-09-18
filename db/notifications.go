package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func PersistNotificationFromCRD(obj *v1.Notification) error {
	ctx := api.NewContext(Gorm, nil)

	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	if obj.Spec.To.Empty() {
		return fmt.Errorf("notification %s has no recipient", obj.Name)
	}

	dbObj := models.Notification{
		ID:         uid,
		Events:     obj.Spec.Events,
		Title:      obj.Spec.Title,
		Template:   obj.Spec.Template,
		Filter:     obj.Spec.Filter,
		Properties: obj.Spec.To.Properties,
	}

	switch {
	case obj.Spec.To.Person != "":
		person, err := duty.FindPerson(ctx, obj.Spec.To.Person)
		if err != nil {
			return err
		} else if person == nil {
			return fmt.Errorf("person (%s) not found", obj.Spec.To.Person)
		}

		dbObj.PersonID = &person.ID

	case obj.Spec.To.Team != "":
		team, err := duty.FindTeam(ctx, obj.Spec.To.Team)
		if err != nil {
			return err
		} else if team == nil {
			return fmt.Errorf("team (%s) not found", obj.Spec.To.Team)
		}

		dbObj.TeamID = &team.ID

	default:
		customService := api.NotificationConfig{
			Name: obj.Name, // Name is mandatory. We derive it from the spec.
		}

		if len(obj.Spec.To.Email) != 0 {
			customService.URL = fmt.Sprintf("smtp://system/?To=%s", obj.Spec.To.Email)
		} else if len(obj.Spec.To.Connection) != 0 {
			customService.Connection = obj.Spec.To.Connection
		} else if len(obj.Spec.To.URL) != 0 {
			customService.URL = obj.Spec.To.URL
		}

		customServices, err := json.Marshal([]api.NotificationConfig{customService})
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

func PersistNotificationSendHistory(ctx *api.Context, h *models.NotificationSendHistory) error {
	return ctx.DB().Save(h).Error
}