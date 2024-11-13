package notification

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
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
				done, err := processPendingNotification(ctx.Context)
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

func processPendingNotification(ctx context.Context) (bool, error) {
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

		var payload NotificationEventPayload
		payload.FromMap(currentHistory.Payload)

		if err := sendPendingNotification(ctx, currentHistory, payload); err != nil {
			if dberr := ctx.DB().Debug().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status":  gorm.Expr("CASE WHEN retries >= ? THEN ? ELSE ? END", ctx.Properties().Int("notification.max-retries", 4)-1, models.NotificationStatusError, models.NotificationStatusPending),
				"error":   err.Error(),
				"retries": gorm.Expr("retries + 1"),
			}).Error; dberr != nil {
				return err
			}

			// return nil
			// or else the transaction will be rolled back and there'll be no trace of a failed attempt.
			return nil
		} else {
			if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status": models.NotificationStatusSent,
			}).Error; dberr != nil {
				return err
			}
		}

		return nil
	})

	return noMorePending, err
}
