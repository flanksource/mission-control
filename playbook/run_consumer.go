package playbook

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/postq"
	"github.com/flanksource/postq/pg"
	"go.opentelemetry.io/otel/trace"
)

func StartPlaybookRunConsumer(ctx context.Context) error {
	return nil // TODO: remove this. This disables the host from running playbook actions.

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
	ctx, ok := c.(context.Context)
	if !ok {
		return 0, errors.New("invalid context")
	}

	var span trace.Span
	ctx, span = ctx.StartSpan("playbook-runs-consumer")
	defer span.End()

	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return 0, fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())

	query := `
		SELECT *
		FROM playbook_runs
		WHERE status IN (?, ?)
			AND scheduled_time <= NOW()
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var runs []models.PlaybookRun
	if err := tx.Raw(query, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusSleeping).Find(&runs).Error; err != nil {
		return 0, err
	}

	for i := range runs {
		ExecuteRun(ctx, runs[i])
	}

	return len(runs), tx.Commit().Error
}
