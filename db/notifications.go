package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	extraClausePlugin "github.com/WinterYukky/gorm-extra-clause-plugin"
	"github.com/WinterYukky/gorm-extra-clause-plugin/exclause"
	"github.com/flanksource/commons/text"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

func DeleteNotificationSilence(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.NotificationSilence{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func PersistNotificationFromCRD(ctx context.Context, obj *v1.Notification) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	if obj.Spec.To.Empty() {
		return fmt.Errorf("notification %s has no recipient", obj.Name)
	}

	dbObj := models.Notification{
		ID:             uid,
		Name:           obj.ObjectMeta.Name,
		Namespace:      obj.ObjectMeta.Namespace,
		Events:         obj.Spec.Events,
		Title:          obj.Spec.Title,
		Template:       obj.Spec.Template,
		Filter:         obj.Spec.Filter,
		Properties:     obj.Spec.To.Properties,
		Source:         models.SourceCRD,
		RepeatInterval: obj.Spec.RepeatInterval,
		GroupBy:        obj.Spec.RepeatGroup,
	}

	if obj.Spec.WaitFor != nil && *obj.Spec.WaitFor != "" {
		if parsed, err := text.ParseDuration(*obj.Spec.WaitFor); err != nil {
			return err
		} else {
			dbObj.WaitFor = parsed
		}
	}

	if obj.Spec.WaitForEvalPeriod != nil && *obj.Spec.WaitForEvalPeriod != "" {
		if parsed, err := text.ParseDuration(*obj.Spec.WaitForEvalPeriod); err != nil {
			return err
		} else {
			dbObj.WaitForEvalPeriod = parsed
		}
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

	case lo.FromPtr(obj.Spec.To.Playbook) != "":
		split := strings.Split(*obj.Spec.To.Playbook, "/")
		if len(split) == 1 {
			name := split[0]
			playbook, err := query.FindPlaybook(ctx, name)
			if err != nil {
				return err
			} else if playbook == nil {
				return fmt.Errorf("playbook (%s) not found", *obj.Spec.To.Playbook)
			}

			dbObj.PlaybookID = &playbook.ID
		} else if len(split) == 2 {
			namespace := split[0]
			name := split[1]

			var playbook models.Playbook
			if err := ctx.DB().Where("namespace = ?", namespace).Where("name = ?", name).Find(&playbook).Error; err != nil {
				return err
			} else if playbook.ID == uuid.Nil {
				return fmt.Errorf("playbook %s not found", *obj.Spec.To.Playbook)
			} else {
				dbObj.PlaybookID = &playbook.ID
			}
		}

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
	return ctx.DB().Model(&models.Notification{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func UpdateNotificationError(ctx context.Context, id string, err string) error {
	return ctx.DB().Model(&models.Notification{}).Where("id = ?", id).Update("error", err).Error
}

func UpdateNotificationSilenceError(ctx context.Context, id string, err string) error {
	return ctx.DB().Model(&models.NotificationSilence{}).Where("id = ?", id).Update("error", err).Error
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

func GetMatchingNotificationSilences(ctx context.Context, resources models.NotificationSilenceResource) ([]models.NotificationSilence, error) {
	_ = ctx.DB().Use(extraClausePlugin.New())

	query := ctx.DB().Model(&models.NotificationSilence{})

	orClauses := ctx.DB().Where("filter != '' OR selectors IS NOT NULL")

	if resources.ConfigID != nil {
		orClauses = orClauses.Or("config_id = ?", *resources.ConfigID)

		// recursive stuff
		orClauses = orClauses.Or("(recursive = true AND path_cte.path LIKE '%' || config_id::TEXT || '%')")
		query = query.Clauses(exclause.NewWith(
			"path_cte",
			ctx.DB().Select("path").Model(&models.ConfigItem{}).Where("id = ?", *resources.ConfigID),
		))
		query = query.Joins("CROSS JOIN path_cte")
	}

	if resources.ComponentID != nil {
		orClauses = orClauses.Or("component_id = ?", *resources.ComponentID)

		// recursive stuff
		orClauses = orClauses.Or("(recursive = true AND path_cte.path LIKE '%' || component_id::TEXT || '%')")
		query = query.Clauses(exclause.NewWith(
			"path_cte",
			ctx.DB().Select("path").Model(&models.Component{}).Where("id = ?", *resources.ComponentID),
		))
		query = query.Joins("CROSS JOIN path_cte")
	}

	if resources.CanaryID != nil {
		orClauses = orClauses.Or("canary_id = ?", *resources.CanaryID)
	}

	if resources.CheckID != nil {
		orClauses = orClauses.Or("check_id = ?", *resources.CheckID)
	}

	query = query.Where(orClauses)

	var silences []models.NotificationSilence
	err := query.Where(`"from" IS NULL OR "from" <= NOW()`).Where("error IS NULL").Where("until IS NULL OR until >= NOW()").Where("deleted_at IS NULL").Find(&silences).Error
	if err != nil {
		return nil, err
	}

	return silences, nil
}

func SaveUnsentNotificationToHistory(ctx context.Context, sendHistory models.NotificationSendHistory) error {
	window := ctx.Properties().Duration("notifications.dedup.window", time.Hour*24)

	return ctx.DB().Exec("SELECT * FROM insert_unsent_notification_to_history(?, ?, ?, ?, ?, ?)",
		sendHistory.NotificationID,
		sendHistory.SourceEvent,
		sendHistory.ResourceID,
		sendHistory.Status,
		window,
		sendHistory.SilencedBy,
	).Error
}
