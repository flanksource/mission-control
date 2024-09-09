package runner

import (
	"fmt"
	"strconv"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
)

// ActionForAgent holds in all the necessary information
// required by an agent to run an action.
type ActionForAgent struct {
	Run         models.PlaybookRun       `json:"run"`
	ActionSpec  v1.PlaybookAction        `json:"action_spec"`
	Action      models.PlaybookRunAction `json:"action"`
	TemplateEnv actions.TemplateEnv      `json:"template_env"`
}

func GetActionForAgent(ctx context.Context, agent *models.Agent) (*ActionForAgent, error) {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())
	ctx = ctx.WithObject(agent)

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

	run, err := step.GetRun(ctx.DB())
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	ctx = ctx.WithObject(agent, step, run)
	playbook, err := step.GetPlaybook(ctx.DB())
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	ctx = ctx.WithObject(playbook, agent, step, run)

	templateEnv, err := CreateTemplateEnv(ctx, playbook, run)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to template env")
	}

	spec, err := getActionSpec(ctx, playbook, step.Name)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	if err := templateActionExpressions(ctx, run, step, spec, templateEnv); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	if spec.TemplatesOn == "" || spec.TemplatesOn == Main {
		if err := TemplateAction(ctx, run, step, spec, templateEnv); err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
	}

	output := ActionForAgent{
		Action:      *step, // step.status will still be waiting
		Run:         *run,
		ActionSpec:  *spec,
		TemplateEnv: templateEnv,
	}

	if skip, err := filterAction(ctx, run.ID, spec.Filter); err != nil {
		return nil, ctx.Oops().Wrap(err)
	} else {
		// We run the filter on the upstream and simply send the filter result to the agent.
		spec.Filter = strconv.FormatBool(!skip)
	}
	// Update the step.status to Running, so that the action is locked and running only on the agent that polled it
	if err := step.Start(ctx.DB()); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	return &output, ctx.Oops().Wrap(tx.Commit().Error)
}
