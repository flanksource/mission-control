package playbook

import (
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

type queueConsumer struct {
	pool                           *pgxpool.Pool
	db                             *gorm.DB
	tickInterval                   time.Duration
	dbReconnectMaxDuration         time.Duration
	dbReconnectBackoffBaseDuration time.Duration

	// registry stores the list of playbook run IDs
	// that are currently being executed.
	registry sync.Map
}

func NewQueueConsumer(db *gorm.DB, pool *pgxpool.Pool) *queueConsumer {
	return &queueConsumer{
		db:                             db,
		pool:                           pool,
		tickInterval:                   time.Minute,
		dbReconnectMaxDuration:         time.Minute,
		dbReconnectBackoffBaseDuration: time.Second,
		registry:                       sync.Map{},
	}
}

func (t *queueConsumer) Listen() error {
	pgNotify := make(chan struct{})
	go utils.ListenToPostgresNotify(db.Pool, "playbook_run_updates", t.dbReconnectMaxDuration, t.dbReconnectBackoffBaseDuration, pgNotify)

	ctx := api.NewContext(t.db, nil)
	for {
		select {
		case <-pgNotify:
			if err := t.consumeAll(ctx); err != nil {
				logger.Errorf("%v", err)
			}

		case <-time.After(t.tickInterval):
			if err := t.consumeAll(ctx); err != nil {
				logger.Errorf("%v", err)
			}
		}
	}
}

func (t *queueConsumer) consumeAll(ctx *api.Context) error {
	runs, err := db.GetScheduledPlaybookRuns(ctx, t.getRunIDsInRegistry()...)
	if err != nil {
		return fmt.Errorf("failed to get playbook runs: %w", err)
	}

	if len(runs) == 0 {
		return nil
	}

	for _, r := range runs {
		go func(run models.PlaybookRun) {
			if _, loaded := t.registry.LoadOrStore(run.ID, nil); !loaded {
				if !run.StartTime.After(time.Now()) {
					time.Sleep(time.Until(run.StartTime))
				}

				ExecuteRun(ctx, run)
			}

			t.registry.Delete(run.ID)
		}(r)
	}

	return nil
}

func (t *queueConsumer) getRunIDsInRegistry() []uuid.UUID {
	var ids []uuid.UUID
	t.registry.Range(func(k any, val any) bool {
		ids = append(ids, k.(uuid.UUID))
		return true
	})

	return ids
}
