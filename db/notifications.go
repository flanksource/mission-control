package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func PersistNotificationFromCRD(ctx context.Context, obj *v1.Notification) error {
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
		Source:     models.SourceCRD,
	}

	switch {
	case obj.Spec.To.Person != "":
		person, err := query.FindPerson(ctx, obj.Spec.To.Person)
		if err != nil {
			return err
		} else if person == nil {
			return fmt.Errorf("person (%s) not found", obj.Spec.To.Person)
		}

		dbObj.PersonID = &person.ID

	case obj.Spec.To.Team != "":
		team, err := query.FindTeam(ctx, obj.Spec.To.Team)
		if err != nil {
			return err
		} else if team == nil {
			return fmt.Errorf("team (%s) not found", obj.Spec.To.Team)
		}

		dbObj.TeamID = &team.ID

	default:
		var customService api.NotificationConfig

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

	return ctx.DB().Save(&dbObj).Error
}

func DeleteNotification(ctx context.Context, id string) error {
	return ctx.DB().Delete(&models.Notification{}, "id = ?", id).Error
}

func UpdateNotificationError(id string, err string) error {
	return Gorm.Model(&models.Notification{}).Where("id = ?", id).Update("error", err).Error
}

func DeleteNotificationSendHistory(ctx context.Context, days int) (int64, error) {
	tx := ctx.DB().
		Model(&models.NotificationSendHistory{}).
		Where(fmt.Sprintf("created_at < NOW() - INTERVAL '%d DAYS'", days)).
		Delete(&models.NotificationSendHistory{})
	return tx.RowsAffected, tx.Error
}

func NotificationSendSummary(ctx context.Context, id string, window time.Duration) (time.Time, int, error) {
	query := `
	SELECT
		min(created_at) AS earliest,
		count(*) AS count
	FROM
		notification_send_history
	WHERE
		notification_id = ?
		AND NOW() - created_at < ?`

	var earliest sql.NullTime
	var count int
	err := ctx.DB().Raw(query, id, window).Row().Scan(&earliest, &count)
	return earliest.Time, count, err
}
