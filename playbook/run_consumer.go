package playbook

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func NewEventQueueConsumer(db *gorm.DB, pool *pgxpool.Pool) *events.EventConsumer {
	return events.NewEventConsumer(db, pool, "playbook_run_updates", EventConsumer).WithNumConsumers(5)
}

func EventConsumer(ctx *api.Context, batchSize int) error {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	query := `
		SELECT *
		FROM playbook_runs
		WHERE status = ?
			AND start_time <= NOW()
		ORDER BY start_time
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`

	var runs []models.PlaybookRun
	if err := tx.Debug().Raw(query, models.PlaybookRunStatusScheduled, batchSize).Find(&runs).Error; err != nil {
		return err
	}

	if len(runs) == 0 {
		return api.Errorf(api.ENOTFOUND, "No events found")
	}

	for i := range runs {
		ExecuteRun(api.NewContext(tx, nil), runs[i])
	}

	return tx.Commit().Error
}
