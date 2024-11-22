package notification

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ProcessPendingNotificationsJob(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "ProcessPendingNotifications",
		Retention:  job.RetentionFailed,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  false,
		Schedule:   "@every 30s",
		Fn: func(ctx job.JobRuntime) error {
			for {
				done, err := ProcessPendingNotifications(ctx.Context)
				if err != nil {
					ctx.History.AddErrorf("failed to send pending notification: %v", err)
					time.Sleep(2 * time.Second) // prevent spinning on db errors
					continue
				}

				ctx.History.IncrSuccess()

				if done {
					break
				}
			}

			return nil
		},
	}
}

func ProcessPendingNotifications(ctx context.Context) (bool, error) {
	var noMorePending bool

	err := ctx.DB().Transaction(func(tx *gorm.DB) error {
		ctx = ctx.WithDB(tx, ctx.Pool())

		var pending []models.NotificationSendHistory
		if err := ctx.DB().Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Options: clause.LockingOptionsSkipLocked}).
			Where("status = ?", models.NotificationStatusPending).
			Where("not_before <= NOW()").
			Where("retries < ? ", ctx.Properties().Int("notification.max-retries", 4)).
			Order("not_before").
			Limit(1). // one at a time; as one notification failure shouldn't affect a previous successful one
			Find(&pending).Error; err != nil {
			return fmt.Errorf("failed to get pending notifications: %w", err)
		}

		if len(pending) == 0 {
			noMorePending = true
			return nil
		}

		currentHistory := pending[0]
		if err := processPendingNotification(ctx, currentHistory); err != nil {
			if dberr := ctx.DB().Debug().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status":  gorm.Expr("CASE WHEN retries >= ? THEN ? ELSE ? END", ctx.Properties().Int("notification.max-retries", 4)-1, models.NotificationStatusError, models.NotificationStatusPending),
				"error":   err.Error(),
				"retries": gorm.Expr("retries + 1"),
			}).Error; dberr != nil {
				return ctx.Oops().Join(dberr, err)
			}
		}

		// we return nil or else the transaction will be rolled back and there'll be no trace of a failed attempt.
		return nil
	})

	return noMorePending, err
}

func processPendingNotification(ctx context.Context, currentHistory models.NotificationSendHistory) error {
	var payload NotificationEventPayload
	payload.FromMap(currentHistory.Payload)

	// We need to re-evaluate the health of the resource.
	// and ensure that the original event matches with the current health before we send out the notification.

	originalEvent := models.Event{Name: payload.EventName, CreatedAt: payload.EventCreatedAt}
	if len(payload.Properties) > 0 {
		if err := json.Unmarshal(payload.Properties, &originalEvent.Properties); err != nil {
			return fmt.Errorf("failed to unmarshal properties: %w", err)
		}
	}

	celEnv, err := GetEnvForEvent(ctx, originalEvent)
	if err != nil {
		return fmt.Errorf("failed to get cel env: %w", err)
	}

	// previousHealth is the health that triggered the notification event
	previousHealth := api.EventToHealth(payload.EventName)

	currentHealth, err := celEnv.GetResourceHealth(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource health from cel env: %w", err)
	}

	notif, err := GetNotification(ctx, payload.NotificationID.String())
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	if !isHealthReportable(notif.Events, previousHealth, currentHealth) {
		ctx.Logger.V(6).Infof("skipping notification[%s] as health change is not reportable", notif.ID)
		if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
			"status": models.NotificationStatusSkipped,
		}).Error; dberr != nil {
			return fmt.Errorf("failed to save notification status as skipped: %w", err)
		}

		return nil
	}

	if err := sendPendingNotification(ctx, currentHistory, payload); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	} else if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
		"status": models.NotificationStatusSent,
	}).Error; dberr != nil {
		return fmt.Errorf("failed to save notification status as sent: %w", err)
	}

	return nil
}
