package runner

import (
	"fmt"
	"strconv"
	"time"

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
	select {
	case <-time.After(LongpollTimeout):
		return &ActionForAgent{}, nil

	case actionID := <-ActionMgr.Register(agent.ID.String()):
		tx := ctx.DB().Begin()
		if tx.Error != nil {
			return nil, fmt.Errorf("error initiating db tx: %w", tx.Error)
		}
		defer tx.Rollback()

		ctx = ctx.WithDB(tx, ctx.Pool())
		ctx = ctx.WithObject(agent)

		var action models.PlaybookRunAction
		if err := ctx.DB().Where("id = ?", actionID).First(&action).Error; err != nil {
			return nil, err
		}

		actionForAgent, err := getAgentAction(ctx, agent, &action)
		if err != nil {
			return nil, err
		}

		return actionForAgent, ctx.Oops().Wrap(tx.Commit().Error)
	}
}

func getAgentAction(ctx context.Context, agent *models.Agent, step *models.PlaybookRunAction) (*ActionForAgent, error) {
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

	return &output, nil
}
