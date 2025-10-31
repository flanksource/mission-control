package playbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/postq"
	"github.com/flanksource/duty/postq/pg"
	"github.com/flanksource/duty/shutdown"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/incident-commander/playbook/runner"
)

type playbookRunError struct {
	RunID    uuid.UUID
	ActionID uuid.UUID
	Err      error
}

func (t *playbookRunError) Error() string {
	return t.Err.Error()
}

// Pg Notify channels for run and action updates
const (
	pgNotifyPlaybookSpecUpdated   = "playbook_spec_updated"
	pgNotifyPlaybookRunUpdates    = "playbook_run_updates"
	pgNotifyPlaybookActionUpdates = "playbook_action_updates"
)

// StartPlaybookConsumers starts the run and action consumers
func StartPlaybookConsumers(ctx context.Context) error {
	runEventConsumer, err := postq.NewPGConsumer(RunConsumer, &postq.ConsumerOption{
		Timeout:      ctx.Properties().Duration("playbook.consumer.timeout", time.Minute),
		NumConsumers: ctx.Properties().Int("playbook.schedulers", 5),
		ErrorHandler: func(cctx context.Context, err error) bool {
			ctx.Errorf("%+v", err)

			var runErr *playbookRunError
			if errors.As(err, &runErr) {
				run := models.PlaybookRun{ID: runErr.RunID}
				if saveErr := run.Fail(ctx.DB(), err); saveErr != nil {
					ctx.Errorf("error updating run status to 'failed': %v", saveErr)
				}
			}

			return false // We do not retry playbook runs
		},
	})

	if err != nil {
		return err
	}

	actionEventConsumer, err := postq.NewPGConsumer(ActionConsumer, &postq.ConsumerOption{
		NumConsumers: ctx.Properties().Int("playbook.action.consumers", 5),
		Timeout:      ctx.Properties().Duration("playbook.consumer.timeout", time.Minute),
		ErrorHandler: func(ctx context.Context, err error) bool {
			logger := ctx.Logger.WithSkipReportLevel(1)
			logger.Errorf("%+v", err)

			var runErr *playbookRunError
			if errors.As(err, &runErr) {

				action := models.PlaybookRunAction{ID: runErr.ActionID}
				if saveErr := action.Fail(ctx.DB(), "", err); saveErr != nil {
					logger.Errorf("error updating playbook action status as failed: %v", err)
				}

				// Need to reschedule the run, so it continues with remaining actions
				run := models.PlaybookRun{ID: runErr.RunID}
				if saveErr := run.Schedule(ctx.DB()); saveErr != nil {
					logger.Errorf("error updating playbook action status as failed: %v", err)
				}
			}

			return false // We do not retry playbook actions
		},
	})
	if err != nil {
		return err
	}

	actionAgentEventConsumer, err := postq.NewPGConsumer(ActionAgentConsumer, &postq.ConsumerOption{
		NumConsumers: ctx.Properties().Int("playbook.action.consumers", 5),
		Timeout:      ctx.Properties().Duration("playbook.consumer.timeout", time.Minute),
		ErrorHandler: func(ctx context.Context, err error) bool {
			logger := ctx.Logger.WithSkipReportLevel(1)
			logger.Errorf("%v", err)

			var runErr *playbookRunError
			if errors.As(err, &runErr) {

				action := models.PlaybookRunAction{ID: runErr.ActionID}
				if saveErr := action.Fail(ctx.DB(), "", err); saveErr != nil {
					logger.Errorf("error updating playbook action status as failed: %v", err)
				}

				// Need to reschedule the run, so it continues with remaining actions
				run := models.PlaybookRun{ID: runErr.RunID}
				if saveErr := run.Schedule(ctx.DB()); saveErr != nil {
					logger.Errorf("error updating playbook action status as failed: %v", err)
				}
			}

			return false // We do not retry playbook actions
		},
	})
	if err != nil {
		return err
	}

	{
		// These channels receive notifications from Postgres on pg_notify
		var (
			runUpdatesPGNotifyChannel         = make(chan string)
			actionUpdatesPGNotifyChannel      = make(chan string)
			actionAgentUpdatesPGNotifyChannel = make(chan string)
			playbookSpecUpdatedChannel        = make(chan string)
		)

		go func() {
			err := pg.ListenMany(ctx,
				pg.ChannelListener{Channel: pgNotifyPlaybookSpecUpdated, Receiver: playbookSpecUpdatedChannel},
				pg.ChannelListener{Channel: pgNotifyPlaybookRunUpdates, Receiver: runUpdatesPGNotifyChannel},
				pg.ChannelListener{Channel: pgNotifyPlaybookActionUpdates, Receiver: actionUpdatesPGNotifyChannel},
				pg.ChannelListener{Channel: pgNotifyPlaybookActionUpdates, Receiver: actionAgentUpdatesPGNotifyChannel},
			)
			if err != nil {
				shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to listen for postgres notifications: %v", err))
			}
		}()

		go func() {
			for range playbookSpecUpdatedChannel {
				clearEventPlaybookCache()
			}
		}()

		go runEventConsumer.Listen(ctx, runUpdatesPGNotifyChannel)
		go actionEventConsumer.Listen(ctx, actionUpdatesPGNotifyChannel)
		go actionAgentEventConsumer.Listen(ctx, actionAgentUpdatesPGNotifyChannel)
	}

	go runner.ActionNotifyRouter.Run(ctx, pgNotifyPlaybookActionUpdates)

	return nil
}

func getActionSpec(ctx context.Context, actionData models.PlaybookActionAgentData, run *models.PlaybookRunAction, templateEnv actions.TemplateEnv) (*v1.PlaybookAction, error) {
	var actionSpec v1.PlaybookAction
	if err := json.Unmarshal(actionData.Spec, &actionSpec); err != nil {
		return nil, err
	}
	actionSpec.PlaybookID = actionData.PlaybookID.String()

	if actionSpec.TemplatesOn == runner.Agent {
		templateEnv.Run = models.PlaybookRun{ID: actionData.RunID, PlaybookID: actionData.PlaybookID}
		templateEnv.Action = run
		if err := runner.TemplateAction(ctx, &actionSpec, templateEnv); err != nil {
			return nil, oops.With(models.ErrorContext(&actionData, run)...).Wrap(err)
		}
	}

	return &actionSpec, nil
}

func getNextActionForAgent(db *gorm.DB) (*models.PlaybookActionAgentData, *models.PlaybookRunAction, error) {
	query := `
		SELECT playbook_action_agent_data.*
		FROM playbook_action_agent_data
		INNER JOIN playbook_run_actions on playbook_run_actions.id = playbook_action_agent_data.action_id
		WHERE status = ?
			AND scheduled_time <= NOW()
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var action []models.PlaybookActionAgentData

	if err := db.Raw(query, models.PlaybookActionStatusWaiting).Find(&action).Error; err != nil {
		return nil, nil, oops.Tags("db").Wrap(err)
	}
	if len(action) == 0 {
		return nil, nil, nil
	}

	run := &models.PlaybookRunAction{ID: action[0].ActionID, PlaybookRunID: action[0].RunID}
	return &action[0], run, nil
}

func getNextAction(db *gorm.DB) (*models.PlaybookRunAction, error) {
	query := `
		SELECT playbook_run_actions.*
		FROM playbook_run_actions
		WHERE status = ?
			AND scheduled_time <= NOW()
			AND (agent_id IS NULL OR agent_id = ?)
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var action []models.PlaybookRunAction
	if err := db.Raw(query, models.PlaybookActionStatusScheduled, uuid.Nil).Find(&action).Error; err != nil {
		return nil, oops.Tags("db").Wrap(err)
	}
	if len(action) == 0 {
		return nil, nil
	}
	return &action[0], nil
}

// ActionConsumer picks up scheduled actions runs scheduled for agents
func ActionAgentConsumer(ctx context.Context) (int, error) {
	if ctx.Properties().On(false, "playbook.runner.disabled") {
		return 0, nil
	}

	ctx.Logger = ctx.Logger.WithSkipReportLevel(-1)

	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		action, run, err := getNextActionForAgent(ctx.DB())
		if err != nil {
			return err
		}
		if action == nil {
			return nil
		}
		ctx = ctx.WithObject(run, action)

		var templateEnv actions.TemplateEnv
		if err := json.Unmarshal(action.Env, &templateEnv); err != nil {
			return oops.Wrap(err)
		}

		spec, err := getActionSpec(ctx, *action, run, templateEnv)
		if err != nil {
			return oops.Wrap(err)
		}

		return runner.ExecuteAndSaveAction(ctx, spec.PlaybookID, run, *spec, templateEnv)
	})

	if err == nil {
		return 0, err
	}
	return 1, err
}

func failOrRetryRun(tx *gorm.DB, run *models.PlaybookRun, err error) error {
	if err == nil {
		return nil
	}
	if e, ok := oops.AsOops(err); ok {
		if lo.Contains(e.Tags(), "db") {
			// DB errors are retryable
			return err
		}
	}
	return run.Fail(tx, err)
}

// ActionConsumer picks up scheduled actions runs them.
func ActionConsumer(parentCtx context.Context) (int, error) {
	if parentCtx.Properties().On(false, "playbook.runner.disabled") {
		return 0, nil
	}

	parentCtx.Logger = parentCtx.Logger.WithSkipReportLevel(-1)

	err := parentCtx.Transaction(func(ctx context.Context, _ trace.Span) error {
		action, err := getNextAction(ctx.DB())
		if err != nil {
			return oops.Wrap(err)
		}
		if action == nil {
			return nil
		}

		ctx = ctx.WithObject(action)

		run, err := action.GetRun(ctx.DB())
		if err != nil {
			return err
		}

		// Create a SAVEPOINT to prevent full transaction rollback
		if err := ctx.DB().Exec("SAVEPOINT action_execution").Error; err != nil {
			return oops.Wrap(err)
		}

		err = runner.RunAction(ctx, run, action)
		if err != nil {
			if IsTxAbortedError(err) {
				if rollbackErr := ctx.DB().Exec("ROLLBACK TO SAVEPOINT action_execution").Error; rollbackErr != nil {
					return oops.Wrapf(rollbackErr, "failed to rollback to savepoint")
				}

				if failErr := action.Fail(ctx.DB(), nil, err); failErr != nil {
					return oops.Wrapf(failErr, "failed to update playbook action with tx abortion error")
				}

				return nil // so we don't rollback
			}

			return err
		}

		return nil
	})

	if err == nil {
		return 0, nil
	}
	return 1, err
}

func IsTxAbortedError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.InFailedSQLTransaction
	}

	return false
}

// RunConsumer picks up scheduled runs and schedules the
// execution of the next action on that run.
func RunConsumer(ctx context.Context) (int, error) {
	if ctx.Properties().On(false, "playbook.scheduler.disabled") {
		return 0, nil
	}

	var consumed = 0
	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		tx := ctx.FastDB()

		query := `
	SELECT playbook_runs.*
	FROM playbook_runs
	INNER JOIN playbooks ON playbooks.id = playbook_runs.playbook_id
	WHERE status IN ? AND scheduled_time <= NOW()
	AND (agent_id IS NULL OR agent_id = ?)
	AND (timeout IS NULL OR timeout > EXTRACT(EPOCH FROM (NOW() - scheduled_time)) * 1000000000)
	ORDER BY scheduled_time
	FOR UPDATE SKIP LOCKED
	LIMIT 1
`

		monitorStatuses := []models.PlaybookRunStatus{
			models.PlaybookRunStatusRetrying,
			models.PlaybookRunStatusScheduled,
			models.PlaybookRunStatusSleeping,
		}
		var run models.PlaybookRun
		if err := tx.Raw(query, monitorStatuses, uuid.Nil).First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return ctx.Oops("db").Wrap(err)
		}
		consumed = 1
		ctx = ctx.WithObject(run)
		if err := runner.ScheduleRun(ctx, run); err != nil {
			return failOrRetryRun(tx, &run, err)
		}
		return nil
	})

	return consumed, err
}
