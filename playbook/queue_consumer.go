package playbook

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
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
	pgNotify := make(chan string)
	go utils.ListenToPostgresNotify(db.Pool, "playbook_run_updates", t.dbReconnectMaxDuration, t.dbReconnectBackoffBaseDuration, pgNotify)

	pgNotifyPlaybookSpecApprovalUpdated := make(chan string)
	go utils.ListenToPostgresNotify(db.Pool, "playbook_spec_approval_updated", t.dbReconnectMaxDuration, t.dbReconnectBackoffBaseDuration, pgNotifyPlaybookSpecApprovalUpdated)

	pgNotifyPlaybookApprovalsInserted := make(chan string)
	go utils.ListenToPostgresNotify(db.Pool, "playbook_approval_inserted", t.dbReconnectMaxDuration, t.dbReconnectBackoffBaseDuration, pgNotifyPlaybookApprovalsInserted)

	ctx := api.NewContext(t.db, nil)
	for {
		select {
		case <-pgNotify:
			if err := t.consumeAll(ctx); err != nil {
				logger.Errorf("%v", err)
			}

		case id := <-pgNotifyPlaybookSpecApprovalUpdated:
			if err := t.onPlaybookSpecApprovalUpdated(ctx, id); err != nil {
				logger.Errorf("%v", err)
			}

		case id := <-pgNotifyPlaybookApprovalsInserted:
			if err := t.onPlaybookRunNewApproval(ctx, id); err != nil {
				logger.Errorf("%v", err)
			}

		case <-time.After(t.tickInterval):
			if err := t.consumeAll(ctx); err != nil {
				logger.Errorf("%v", err)
			}
		}
	}
}

func (t *queueConsumer) onPlaybookSpecApprovalUpdated(ctx *api.Context, playbookID string) error {
	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", playbookID).First(&playbook).Error; err != nil {
		return err
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil {
		return nil
	}

	return db.UpdatePlaybookRunStatusIfApproved(ctx, playbookID, *spec.Approval)
}

func (t *queueConsumer) onPlaybookRunNewApproval(ctx *api.Context, runID string) error {
	var run models.PlaybookRun
	if err := ctx.DB().Where("id = ?", runID).First(&run).Error; err != nil {
		return err
	}

	if run.Status != models.PlaybookRunStatusPending {
		return nil
	}

	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbook).Error; err != nil {
		return err
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil {
		return nil
	}

	return db.UpdatePlaybookRunStatusIfApproved(ctx, playbook.ID.String(), *spec.Approval)
}

func (t *queueConsumer) consumeAll(ctx *api.Context) error {
	runs, err := db.GetScheduledPlaybookRuns(ctx, time.Minute*10, t.getRunIDsInRegistry()...)
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
