package playbook

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/postq"
	"github.com/flanksource/postq/pg"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

// Pg Notify channels for run and action updates
const (
	pgNotifyPlaybookRunUpdates    = "playbook_run_updates"
	pgNotifyPlaybookActionUpdates = "playbook_action_updates"
)

// StartPlaybookConsumers starts the run and action consumers
func StartPlaybookConsumers(ctx context.Context) error {
	runEventConsumer, err := postq.NewPGConsumer(RunConsumer, &postq.ConsumerOption{NumConsumers: 5})
	if err != nil {
		return err
	}

	actionEventConsumer, err := postq.NewPGConsumer(ActionConsumer, &postq.ConsumerOption{NumConsumers: 50})
	if err != nil {
		return err
	}

	runUpdatesPGNotifyChannel := make(chan string)
	go pg.Listen(ctx, pgNotifyPlaybookRunUpdates, runUpdatesPGNotifyChannel)
	go runEventConsumer.Listen(ctx, runUpdatesPGNotifyChannel)

	actionUpdatesPGNotifyChannel := make(chan string)
	go pg.Listen(ctx, pgNotifyPlaybookActionUpdates, actionUpdatesPGNotifyChannel)
	go actionEventConsumer.Listen(ctx, actionUpdatesPGNotifyChannel)

	return nil
}

// RunConsumer picks up scheduled runs and schedules the
// execution of the next action on that run.
func ActionConsumer(c postq.Context) (int, error) {
	ctx, ok := c.(context.Context)
	if !ok {
		return 0, errors.New("invalid context")
	}

	ctx.Debugf("consuming playbook action ...")

	var span trace.Span
	ctx, span = ctx.StartSpan("playbook-action-consumer")
	defer span.End()

	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return 0, fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())

	query := `
		SELECT playbook_run_actions.*
		FROM playbook_run_actions
		WHERE status IN (?, ?)
			AND scheduled_time <= NOW()
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var foundActions []models.PlaybookRunAction
	if err := tx.Raw(query, models.PlaybookActionStatusScheduled, models.PlaybookActionStatusSleeping).Find(&foundActions).Error; err != nil {
		return 0, err
	}

	for i := range foundActions {
		// Agent doesn't have a run associated with the action.
		// So we skip templating, as the upstream does that before sending the action.
		if foundActions[i].PlaybookRunID == uuid.Nil {
			var actionTemplate models.PlaybookActionAgentData
			if err := ctx.DB().Where("action_id = ?", foundActions[i].ID).First(&actionTemplate).Error; err != nil {
				return 0, err
			}

			var templateEnv actions.TemplateEnv
			if err := json.Unmarshal(actionTemplate.Env, &templateEnv); err != nil {
				return 0, err
			}

			var actionSpec v1.PlaybookAction
			if err := json.Unmarshal(actionTemplate.Spec, &actionSpec); err != nil {
				return 0, err
			}

			if err := executeAndSaveAction(ctx, actionTemplate.PlaybookID, actionTemplate.RunID, foundActions[i], actionSpec, templateEnv); err != nil {
				return 0, err
			}

			continue
		}

		var playbook models.Playbook
		if err := ctx.DB().Table("playbook_runs").
			Select("playbooks.*").
			Joins("LEFT JOIN playbooks ON playbooks.id = playbook_runs.playbook_id").
			Where("playbook_runs.id = ?", foundActions[i].PlaybookRunID).
			First(&playbook).Error; err != nil {
			return 0, fmt.Errorf("failed to get the playbook for the given action(%s): %w", foundActions[i].ID, err)
		}

		var playbookSpec v1.PlaybookSpec
		if err := json.Unmarshal(playbook.Spec, &playbookSpec); err != nil {
			return 0, fmt.Errorf("failed to unmarshal playbook spec: %w", err)
		}

		var run models.PlaybookRun
		if err := ctx.DB().Where("id = ?", foundActions[i].PlaybookRunID).First(&run).Error; err != nil {
			return 0, fmt.Errorf("failed to get the playbook run for the given action(%s): %w", foundActions[i].ID, err)
		}

		for _, action := range playbookSpec.Actions {
			if action.Name == foundActions[i].Name {
				if err := templateAndExecuteAction(ctx, run, foundActions[i], action); err != nil {
					return 0, err
				}

				break
			}
		}
	}

	return len(foundActions), tx.Commit().Error
}

// RunConsumer picks up scheduled runs and schedules the
// execution of the next action on that run.
func RunConsumer(c postq.Context) (int, error) {
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
		SELECT playbook_runs.*
		FROM playbook_runs
		INNER JOIN playbooks ON playbooks.id = playbook_runs.playbook_id
		WHERE status = ?
			AND scheduled_time <= NOW()
			AND (playbooks.spec->'runsOn' @> ? OR playbooks.spec->'runsOn' IS NULL)
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var runs []models.PlaybookRun
	if err := tx.Raw(query, models.PlaybookRunStatusScheduled, fmt.Sprintf(`["%s"]`, hostRunner)).Find(&runs).Error; err != nil {
		return 0, err
	}

	for i := range runs {
		if err := HandleRun(ctx, runs[i]); err != nil {
			return 0, fmt.Errorf("failed to schedule the next action for run %d: %w", runs[i].ID, err)
		}
	}

	return len(runs), tx.Commit().Error
}
