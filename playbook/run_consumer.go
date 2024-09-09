package playbook

import (
	"encoding/json"
	"errors"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/postq"
	"github.com/flanksource/duty/postq/pg"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
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
	pgNotifyPlaybookRunUpdates    = "playbook_run_updates"
	pgNotifyPlaybookActionUpdates = "playbook_action_updates"
)

// StartPlaybookConsumers starts the run and action consumers
func StartPlaybookConsumers(ctx context.Context) error {
	runEventConsumer, err := postq.NewPGConsumer(RunConsumer, &postq.ConsumerOption{
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

	runUpdatesPGNotifyChannel := make(chan string)
	go pg.Listen(ctx, pgNotifyPlaybookRunUpdates, runUpdatesPGNotifyChannel)
	go runEventConsumer.Listen(ctx, runUpdatesPGNotifyChannel)

	actionUpdatesPGNotifyChannel := make(chan string)
	go pg.Listen(ctx, pgNotifyPlaybookActionUpdates, actionUpdatesPGNotifyChannel)
	go actionEventConsumer.Listen(ctx, actionUpdatesPGNotifyChannel)

	actionAgentUpdatesPGNotifyChannel := make(chan string)
	go pg.Listen(ctx, pgNotifyPlaybookActionUpdates, actionAgentUpdatesPGNotifyChannel)
	go actionAgentEventConsumer.Listen(ctx, actionAgentUpdatesPGNotifyChannel)

	return nil
}

func getActionSpec(ctx context.Context, actionData models.PlaybookActionAgentData, run *models.PlaybookRunAction) (*v1.PlaybookAction, error) {
	var actionSpec v1.PlaybookAction
	if err := json.Unmarshal(actionData.Spec, &actionSpec); err != nil {
		return nil, err
	}
	actionSpec.PlaybookID = actionData.PlaybookID.String()

	var templateEnv actions.TemplateEnv
	if err := json.Unmarshal(actionData.Env, &templateEnv); err != nil {
		return nil, oops.With(models.ErrorContext(&actionData, run)...).Wrap(err)
	}

	if actionSpec.TemplatesOn == runner.Agent {
		if err := runner.TemplateAction(ctx, &models.PlaybookRun{ID: actionData.RunID, PlaybookID: actionData.PlaybookID}, run, &actionSpec, templateEnv); err != nil {
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
		spec, err := getActionSpec(ctx, *action, run)
		if err != nil {
			return oops.Wrap(err)
		}

		return runner.ExecuteAndSaveAction(ctx, spec.PlaybookID, run, *spec)
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
func ActionConsumer(ctx context.Context) (int, error) {
	if ctx.Properties().On(false, "playbook.runner.disabled") {
		return 0, nil
	}

	ctx.Logger = ctx.Logger.WithSkipReportLevel(-1)

	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		tx := ctx.DB()
		action, err := getNextAction(tx)
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

		if err := runner.RunAction(ctx, run, action); err != nil {
			return err
		}

		return nil
	})

	if err == nil {
		return 0, nil
	}
	return 1, err
}

// RunConsumer picks up scheduled runs and schedules the
// execution of the next action on that run.
func RunConsumer(ctx context.Context) (int, error) {
	if ctx.Properties().On(false, "playbook.scheduler.disabled") {
		return 0, nil
	}
	var consumed = 0
	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		tx := ctx.FastDB("playbook.consumer")

		query := `
	SELECT playbook_runs.*
	FROM playbook_runs
	INNER JOIN playbooks ON playbooks.id = playbook_runs.playbook_id
	WHERE status IN (?, ?) AND scheduled_time <= NOW()
	ORDER BY scheduled_time
	FOR UPDATE SKIP LOCKED
	LIMIT 1
`
		var run models.PlaybookRun
		if err := tx.Raw(query, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusSleeping).First(&run).Error; err != nil {
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
