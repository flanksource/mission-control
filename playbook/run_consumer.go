package playbook

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events/eventconsumer"
	"github.com/flanksource/postq/pg"
	"go.opentelemetry.io/otel"
)

func StartPlaybookRunConsumer(ctx api.Context) {
	pgNotifyChannel := make(chan string)
	go pg.Listen(ctx, "playbook_run_updates", pgNotifyChannel)

	eventconsumer.New(EventConsumer).
		WithNumConsumers(5).
		Listen(ctx, pgNotifyChannel)
}

func EventConsumer(ctx api.Context) (int, error) {
	tracer := otel.GetTracerProvider().Tracer("event-tracer")
	traceCtx, span := tracer.Start(ctx, "playbook-runs-consumer")
	ctx = ctx.WithContext(traceCtx)
	defer span.End()

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
