package playbook

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events/eventconsumer"
	"github.com/flanksource/incident-commander/utils"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func StartPlaybookRunConsumer(db *gorm.DB, pool *pgxpool.Pool) {
	const (
		dbReconnectMaxDuration         = time.Minute * 5
		dbReconnectBackoffBaseDuration = time.Second
	)

	pgNotifyChannel := make(chan string)
	go utils.ListenToPostgresNotify(pool, "playbook_run_updates", dbReconnectMaxDuration, dbReconnectBackoffBaseDuration, pgNotifyChannel)

	eventconsumer.New(db, pool, EventConsumer).
		WithNumConsumers(5).
		Listen(pgNotifyChannel)
}

func EventConsumer(ctx *api.Context) (int, error) {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return 0, fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx)

	query := `
		SELECT *
		FROM playbook_runs
		WHERE status = ?
			AND start_time <= NOW()
		ORDER BY start_time
		FOR UPDATE SKIP LOCKED
	`

	var runs []models.PlaybookRun
	if err := tx.Raw(query, models.PlaybookRunStatusScheduled).Find(&runs).Error; err != nil {
		return 0, err
	}

	for i := range runs {
		ExecuteRun(ctx, runs[i])
	}

	return len(runs), tx.Commit().Error
}
