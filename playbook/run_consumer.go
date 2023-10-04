package playbook

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/postq"
	"github.com/flanksource/postq/pg"
	"go.opentelemetry.io/otel"
)

func StartPlaybookRunConsumer(ctx api.Context) error {
	ec, err := postq.NewPGConsumer(EventConsumer, &postq.ConsumerOption{
		NumConsumers: 5,
	})
	if err != nil {
		return err
	}

	pgNotifyChannel := make(chan string)
	go pg.Listen(ctx, "playbook_run_updates", pgNotifyChannel)

	go ec.Listen(ctx, pgNotifyChannel)
	return nil
}

func EventConsumer(c postq.Context) (int, error) {
	ctx, ok := c.(api.Context)
	if !ok {
		return 0, errors.New("invalid context")
	}

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
