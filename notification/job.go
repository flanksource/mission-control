package notification

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
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
			tx := ctx.DB().Begin()
			if tx.Error != nil {
				return nil
			}
			defer tx.Rollback()

			var pending []models.NotificationSendHistory
			if err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Options: clause.LockingOptionsSkipLocked}).
				Where("status = ?", models.NotificationStatusPending).
				Where("delay IS NULL OR created_at + (delay * INTERVAL '1 second' / 1000000000)  <= NOW()").
				Order("created_at + (delay * INTERVAL '1 second' / 1000000000)"). // smallest effective send time first
				Limit(1).                                                         // one at a time; as one notification failure shouldn't affect a previous successful one
				Find(&pending).Error; err != nil {
				return fmt.Errorf("failed to get pending notifications: %w", err)
			}

			ctx.Infof("Pending notifications: %d", len(pending))

			return tx.Commit().Error
		},
	}
}
