package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	extraClausePlugin "github.com/WinterYukky/gorm-extra-clause-plugin"
	"github.com/WinterYukky/gorm-extra-clause-plugin/exclause"
	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/text"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
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
		GroupBy:        obj.Spec.GroupBy,
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

	if len(obj.Spec.GroupBy) > 0 && obj.Spec.WaitFor != nil && *obj.Spec.WaitFor == "" {
		return fmt.Errorf("groupBy provided with an empty waitFor. either remove the groupBy or set a waitFor period")
	}

	if recipient, err := resolveNotificationRecipient(ctx, obj.Spec.To); err != nil {
		return fmt.Errorf("failed to resolve recipient: %w", err)
	} else {
		dbObj.PersonID = recipient.PersonID
		dbObj.TeamID = recipient.TeamID
		dbObj.PlaybookID = recipient.PlaybookID
		dbObj.CustomServices = recipient.CustomServices
	}

	if obj.Spec.Inhibitions != nil {
		if b, err := json.Marshal(obj.Spec.Inhibitions); err != nil {
			return fmt.Errorf("failed to marshal inhibitions: %w", err)
		} else {
			dbObj.Inhibitions = b
		}
	}

	if obj.Spec.Fallback != nil {
		if recipient, err := resolveNotificationRecipient(ctx, obj.Spec.Fallback.NotificationRecipientSpec); err != nil {
			return fmt.Errorf("failed to resolve recipient: %w", err)
		} else {
			dbObj.FallbackPersonID = recipient.PersonID
			dbObj.FallbackTeamID = recipient.TeamID
			dbObj.FallbackPlaybookID = recipient.PlaybookID
			dbObj.FallbackCustomServices = recipient.CustomServices
		}

		if obj.Spec.Fallback.Delay != "" {
			parsed, err := duration.ParseDuration(obj.Spec.Fallback.Delay)
			if err != nil {
				return fmt.Errorf("failed to parse fallback delay: %w", err)
			}

			dbObj.FallbackDelay = lo.ToPtr(time.Duration(parsed))
		}
	}

	return ctx.DB().Save(&dbObj).Error
}

type notificationRecipient struct {
	PersonID       *uuid.UUID
	TeamID         *uuid.UUID
	PlaybookID     *uuid.UUID
	CustomServices types.JSON
}

func resolveNotificationRecipient(ctx context.Context, recipient v1.NotificationRecipientSpec) (*notificationRecipient, error) {
	var result notificationRecipient
	switch {
	case recipient.Person != "":
		person, err := query.FindPerson(ctx, recipient.Person)
		if err != nil {
			return nil, err
		} else if person == nil {
			return nil, fmt.Errorf("person (%s) not found", recipient.Person)
		}

		result.PersonID = &person.ID

	case recipient.Team != "":
		team, err := query.FindTeam(ctx, recipient.Team)
		if err != nil {
			return nil, err
		} else if team == nil {
			return nil, fmt.Errorf("team (%s) not found", recipient.Team)
		}

		result.TeamID = &team.ID

	case lo.FromPtr(recipient.Playbook) != "":
		split := strings.Split(*recipient.Playbook, "/")
		if len(split) == 1 {
			name := split[0]
			playbook, err := query.FindPlaybook(ctx, name)
			if err != nil {
				return nil, err
			} else if playbook == nil {
				return nil, fmt.Errorf("playbook (%s) not found", *recipient.Playbook)
			}

			result.PlaybookID = &playbook.ID
		} else if len(split) == 2 {
			namespace := split[0]
			name := split[1]

			var playbook models.Playbook
			if err := ctx.DB().Where("namespace = ?", namespace).Where("name = ?", name).Where("deleted_at IS NULL").Find(&playbook).Error; err != nil {
				return nil, err
			} else if playbook.ID == uuid.Nil {
				return nil, fmt.Errorf("playbook %s not found", *recipient.Playbook)
			} else {
				result.PlaybookID = &playbook.ID
			}
		}

	default:
		var customService api.NotificationConfig

		if len(recipient.Email) != 0 {
			customService.URL = fmt.Sprintf("smtp://system/?To=%s", recipient.Email)
		} else if len(recipient.Connection) != 0 {
			customService.Connection = recipient.Connection
		} else if len(recipient.URL) != 0 {
			customService.URL = recipient.URL
		}

		customServices, err := json.Marshal([]api.NotificationConfig{customService})
		if err != nil {
			return nil, err
		}

		result.CustomServices = customServices
	}

	return &result, nil
}

func DeleteNotification(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Notification{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func DeleteStaleNotification(ctx context.Context, newer *v1.Notification) error {
	return ctx.DB().Model(&models.Notification{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
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
		Where("playbook_run_id IS NULL").
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

	return ctx.DB().Exec("SELECT * FROM insert_unsent_notification_to_history(?, ?, ?, ?, ?, ?, ?)",
		sendHistory.NotificationID,
		sendHistory.SourceEvent,
		sendHistory.ResourceID,
		sendHistory.Status,
		window,
		sendHistory.SilencedBy,
		sendHistory.ParentID,
	).Error
}
