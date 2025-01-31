package runner

import (
	"strconv"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"gorm.io/gorm"
)

const errTagTemplate = "template-error"

// ActionForAgent holds in all the necessary information
// required by an agent to run an action.
type ActionForAgent struct {
	Run         models.PlaybookRun       `json:"run"`
	ActionSpec  v1.PlaybookAction        `json:"action_spec"`
	Action      models.PlaybookRunAction `json:"action"`
	TemplateEnv actions.TemplateEnv      `json:"template_env"`
}

func GetActionForAgentWithWait(ctx context.Context, agent *models.Agent) (*ActionForAgent, error) {
	action, err := getActionForAgent(ctx, agent)
	if err != nil {
		return nil, err
	}

	if action != nil {
		return action, err
	}

	// Go into waiting state
	select {
	case <-time.After(ctx.Properties().Duration("playbook.runner.longpoll.timeout", DefaultLongpollTimeout)):
		return &ActionForAgent{}, nil

	case <-ActionNotifyRouter.GetOrCreateChannel(agent.ID.String()):
		action, err := getActionForAgent(ctx, agent)
		if err != nil {
			return nil, err
		}

		return action, err
	}
}

func getActionForAgent(ctx context.Context, agent *models.Agent) (*ActionForAgent, error) {
	var output *ActionForAgent

	err := ctx.DB().Transaction(func(tx *gorm.DB) error {
		ctx = ctx.WithDB(tx, ctx.Pool()).WithObject(agent)

		result, err := _getActionForAgent(ctx, agent)
		if err != nil {
			if oopsErr, ok := oops.AsOops(err); ok {
				if lo.Contains(oopsErr.Tags(), errTagTemplate) {
					// mark the action as failed and move on
					return result.Action.Fail(tx, nil, err)
				}
			}

			return err
		}

		output = result
		return nil
	})
	if err != nil {
		return nil, err
	}

	return output, nil
}

func _getActionForAgent(ctx context.Context, agent *models.Agent) (*ActionForAgent, error) {
	query := `
		SELECT playbook_run_actions.*
		FROM playbook_run_actions
		INNER JOIN playbook_runs ON playbook_runs.id = playbook_run_actions.playbook_run_id
		INNER JOIN playbooks ON playbooks.id = playbook_runs.playbook_id
		WHERE playbook_run_actions.status = ?
			AND (playbook_run_actions.scheduled_time IS NULL or playbook_run_actions.scheduled_time <= NOW())
			AND playbook_run_actions.agent_id = ?
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var steps []models.PlaybookRunAction
	if err := ctx.DB().Raw(query, models.PlaybookRunStatusWaiting, agent.ID).Find(&steps).Error; err != nil {
		return nil, ctx.Oops("db").Wrap(err)
	}

	if len(steps) == 0 {
		return nil, nil
	}

	step := &steps[0]
	ctx = ctx.WithObject(agent, step)

	output := ActionForAgent{
		Action: *step, // step.status will still be waiting
	}

	run, err := step.GetRun(ctx.DB())
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	output.Run = *run
	ctx = ctx.WithObject(agent, step, run)

	playbook, err := step.GetPlaybook(ctx.DB())
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	ctx = ctx.WithObject(playbook, agent, step, run)

	templateEnv, err := CreateTemplateEnv(ctx, playbook, *run, step)
	if err != nil {
		return &output, ctx.Oops().Tags(errTagTemplate).Wrapf(err, "failed to create template env")
	}
	output.TemplateEnv = templateEnv

	spec, err := getActionSpec(run, step.Name)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	if err := templateActionExpressions(ctx, spec, templateEnv); err != nil {
		return &output, ctx.Oops().Tags(errTagTemplate).Wrapf(err, "failed to template action expressions")
	}

	if spec.TemplatesOn == "" || spec.TemplatesOn == Main {
		if err := TemplateAction(ctx, spec, templateEnv); err != nil {
			return &output, ctx.Oops().Tags(errTagTemplate).Wrapf(err, "failed to template action")
		}
	}
	output.ActionSpec = *spec

	if skip, err := filterAction(ctx, spec.Filter); err != nil {
		return &output, ctx.Oops().Tags(errTagTemplate).Wrapf(err, "action filter error")
	} else {
		// We run the filter on the upstream and simply send the filter result to the agent.
		spec.Filter = strconv.FormatBool(!skip)
	}

	// Update the step.status to Running, so that the action is locked and running only on the agent that polled it
	if err := step.Start(ctx.DB()); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	return &output, nil
}
