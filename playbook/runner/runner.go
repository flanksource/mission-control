package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/config/schemas"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"gorm.io/gorm"
)

const (
	// Main is the identifier for the upstream server.
	Main = "local"

	// Agent is the identifier for any agent runner.
	Agent = "agent"
)

func GetNextActionToRun(ctx context.Context, run models.PlaybookRun) (action *v1.PlaybookAction, lastAction *models.PlaybookRunAction, err error) {
	ctx.Logger.V(3).Infof("getting next action for run %s", run.ID)
	ctx = ctx.WithObject(run)

	if validationErr, err := schemas.ValidatePlaybookSpec(run.Spec); err != nil {
		return nil, nil, err
	} else if validationErr != nil {
		return nil, nil, validationErr
	}

	var playbookSpec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &playbookSpec); err != nil {
		return nil, nil, ctx.Oops().Wrap(err)
	}

	var lastRanAction models.PlaybookRunAction
	if err := ctx.DB().Model(&models.PlaybookRunAction{}).
		Where("playbook_run_id = ?", run.ID).
		Where("status IN ?", models.PlaybookActionFinalStates).
		Order("start_time DESC").
		First(&lastRanAction).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ctx.Oops("db").Wrap(err)
		}
	}

	if lastRanAction.Name == "" { // Nothing has run yet
		ctx.Logger.V(4).Infof("no previous action, running first action")
		return &playbookSpec.Actions[0], nil, nil
	}

	for i, action := range playbookSpec.Actions {
		if action.Name == lastRanAction.Name {
			// If last action failed do not run more steps unless it has a filter or a retry policy
			if lastRanAction.Status == models.PlaybookActionStatusFailed {
				canRetry := action.Retry != nil && lastRanAction.RetryCount < action.Retry.Limit
				if canRetry {
					return &playbookSpec.Actions[i], &lastRanAction, nil
				}

				alwaysAction := findNextActionWithFilter(playbookSpec.Actions[i+1:])
				return alwaysAction, &lastRanAction, nil
			}

			return &playbookSpec.Actions[i+1], &lastRanAction, nil
		}

		if i == len(playbookSpec.Actions)-1 {
			// return if this is the last action.
			return nil, nil, nil
		}
	}

	return nil, nil, ctx.Oops("db").Errorf("could not find action to run or complete")
}

func findNextActionWithFilter(actions []v1.PlaybookAction) *v1.PlaybookAction {
	for _, action := range actions {
		if action.Filter != "" {
			return &action
		}
	}
	return nil
}

func getActionSpec(run *models.PlaybookRun, name string) (*v1.PlaybookAction, error) {
	var spec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &spec); err != nil {
		return nil, err
	}

	for _, action := range spec.Actions {
		if action.Name == name {
			action.PlaybookID = run.PlaybookID.String()
			return &action, nil
		}
	}

	return nil, nil
}

func CheckPlaybookFilter(ctx context.Context, playbookSpec v1.PlaybookSpec, templateEnv actions.TemplateEnv) error {
	for _, f := range playbookSpec.Filters {
		val, err := ctx.RunTemplate(gomplate.Template{Expression: f}, templateEnv.AsMap(ctx))
		if err != nil {
			return ctx.Oops().Wrapf(err, "invalid playbook filter: %s", f)

		}

		// The expression must return a boolean
		if val != "true" {
			return ctx.Oops().Errorf("%s", val)
		}
	}
	return nil
}

func GetDelay(ctx context.Context, playbook models.Playbook, run models.PlaybookRun, action *v1.PlaybookAction, lastRan *models.PlaybookRunAction) (time.Duration, error) {
	// The delays on the action should be applied here & action consumers do not run the delay.
	var delay time.Duration
	if action.Delay != "" && run.Status != models.PlaybookRunStatusSleeping {
		templateEnv, err := CreateTemplateEnv(ctx, &playbook, run, lastRan)
		if err != nil {
			return 0, ctx.Oops().Wrapf(err, "failed to template action")
		}
		oops := ctx.Oops().Hint(templateEnv.JSON(ctx))
		if action.Delay, err = ctx.RunTemplate(gomplate.Template{Expression: action.Delay}, templateEnv.AsMap(ctx)); err != nil {
			return 0, oops.Wrapf(err, "failed to template action")
		} else if delay, err = action.DelayDuration(); err != nil {
			return 0, oops.Wrapf(err, "invalid duration n (%s)", action.Delay)
		}
	}

	return delay, nil
}

func getEligibleAgents(spec v1.PlaybookSpec, action *v1.PlaybookAction, run models.PlaybookRun) []string {
	if run.AgentID != nil {
		return []string{run.AgentID.String()}
	}

	if len(action.RunsOn) != 0 {
		return action.RunsOn
	}

	if len(spec.RunsOn) != 0 {
		return spec.RunsOn
	}

	return []string{Main}
}

// ScheduleRun finds the next action step that needs to run and
// creates the PlaybookActionRun in a scheduled status, with an optional agentId
func ScheduleRun(ctx context.Context, run models.PlaybookRun) error {
	var playbook models.Playbook
	if err := ctx.DB().First(&playbook, run.PlaybookID).Error; err != nil {
		return ctx.Oops("db").Wrap(err)
	}

	// Override the current spec with the run's spec
	playbook.Spec = run.Spec

	ctx = ctx.WithObject(run)

	action, lastRan, err := GetNextActionToRun(ctx, run)
	if err != nil {
		ctx.Tracef("Unable to get next action")
		return ctx.Oops().Wrap(err)
	}
	if action == nil {
		return ctx.Oops("db").Wrap(run.End(ctx.DB()))
	}

	ctx = ctx.WithObject(action, run)

	isRetrying := lastRan != nil && lastRan.Name == action.Name && action.Retry != nil
	if isRetrying && run.Status != models.PlaybookRunStatusRetrying {
		delay, err := action.Retry.NextRetryWait(lastRan.RetryCount + 1)
		if err != nil {
			return ctx.Oops().Wrap(err)
		}

		if delay > 0 {
			ctx.Tracef("delaying %s by %s", action.Name, delay)
			return ctx.Oops().Wrap(run.Retry(ctx.DB(), delay))
		}
	}

	if delay, err := GetDelay(ctx, playbook, run, action, lastRan); err != nil {
		return err
	} else if delay > 0 {
		ctx.Tracef("delaying %s by %s", action.Name, delay)
		return ctx.Oops().Wrap(run.Delay(ctx.DB(), delay))
	}

	if run.AgentID != nil {
		ctx.Tracef("action already assigning to %s", run.AgentID.String())
		return ctx.Oops("db").Wrap(run.Assign(ctx.DB(), &models.Agent{
			ID: *run.AgentID,
		}, action.Name))
	}

	var playbookSpec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &playbookSpec); err != nil {
		return ctx.Oops().Wrap(err)
	}

	eligibleAgents := getEligibleAgents(playbookSpec, action, run)

	agent, err := db.FindFirstAgent(ctx, eligibleAgents...)
	if err != nil {
		return ctx.Oops("db").Wrap(err)
	} else if agent == nil {
		return ctx.Oops().Errorf("failed to find any agent (%s)", strings.Join(eligibleAgents, ","))
	}

	if agent.Name == Main {
		if isRetrying {
			if runAction, err := run.RetryAction(ctx.DB(), action.Name, lastRan.RetryCount+1); err != nil {
				return ctx.Oops("db").Wrap(err)
			} else {
				ctx.Tracef("started %s (%v) on local", action.Name, runAction.ID)
			}
		} else {
			if runAction, err := run.StartAction(ctx.DB(), action.Name); err != nil {
				return ctx.Oops("db").Wrap(err)
			} else {
				ctx.Tracef("started %s (%v) on local", action.Name, runAction.ID)
			}
		}
	} else {
		// Assign the action to an agent and step the status to Waiting
		// When the agent polls for new actions to run, we return and then set the status to Running
		ctx.Tracef("assigning %s to agent %s", action.Name, agent.Name)
		return ctx.Oops("db").Wrap(run.Assign(ctx.DB(), agent, action.Name))
	}

	return nil
}

func ExecuteAndSaveAction(ctx context.Context, playbookID any, action *models.PlaybookRunAction, actionSpec v1.PlaybookAction) error {
	db := ctx.DB()

	if err := action.Start(db); err != nil {
		return ctx.Oops().Wrap(err)
	}

	result, err := executeAction(ctx, playbookID, action.PlaybookRunID, *action, actionSpec)
	if err != nil {
		ctx.Errorf("action failed %+v", err)
		if err := action.Fail(db, result.data, err); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	} else if result.skipped {
		if err := action.Skip(db); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	} else if accessor, ok := result.data.(StatusAccessor); ok && accessor.GetStatus() == models.PlaybookActionStatusFailed {
		ctx.Warnf("action returned failure\n%v", logger.Pretty(result.data))
		if err := action.Fail(db, result.data, nil); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	} else {
		ctx.Tracef("action completed\n%v", logger.Pretty(result.data))
		if err := action.Complete(db, result.data); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	}

	return nil

}

func RunAction(ctx context.Context, run *models.PlaybookRun, action *models.PlaybookRunAction) error {
	playbook, err := action.GetPlaybook(ctx.DB())
	if err != nil {
		return err
	} else if playbook == nil {
		return ctx.Oops().Errorf("playbook not found")
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal([]byte(run.Spec), &spec); err != nil {
		return err
	}

	ctx = ctx.WithObject(action, run)
	ctx, span := ctx.StartSpan(fmt.Sprintf("playbook.%s", playbook.Name))
	defer span.End()

	if err := TemplateAndExecuteAction(ctx, spec, playbook, run, action); err != nil {
		if e, ok := oops.AsOops(err); ok {
			if lo.Contains(e.Tags(), "db") {
				// DB errors are retryable
				return err
			}
		}

		if ctx.IsTrace() {
			ctx.Errorf("action failed: %+v", err)
		} else if ctx.IsDebug() {
			ctx.Errorf("action failed: %v", err)
		} else {
			ctx.Infof("action failed: %v", err)
		}

		return action.Fail(ctx.DB(), "", err)

	}
	return nil
}

// TemplateAndExecuteAction executes the given playbook action after templating it.
func TemplateAndExecuteAction(ctx context.Context, spec v1.PlaybookSpec, playbook *models.Playbook, run *models.PlaybookRun, action *models.PlaybookRunAction) error {
	ctx = ctx.WithObject(playbook, run, action)

	step, found := lo.Find(spec.Actions, func(i v1.PlaybookAction) bool { return i.Name == action.Name })
	if !found {
		return ctx.Oops().Errorf("action '%s' not found", action.Name)
	}

	templateEnv, err := CreateTemplateEnv(ctx, playbook, *run, action)
	if err != nil {
		return err
	}

	oops := ctx.Oops().Hint(templateEnv.JSON(ctx))

	ctx.Logger.V(7).Infof("Using env: %s", logger.Pretty(templateEnv.Env))

	if err := templateActionExpressions(ctx, &step, templateEnv); err != nil {
		return err
	}

	if err := TemplateAction(ctx, &step, templateEnv); err != nil {
		return err
	}

	if step.AI != nil && step.AI.Config == "" {
		step.AI.Config = run.ConfigID.String()
	}

	return oops.Wrap(ExecuteAndSaveAction(ctx, run.PlaybookID, action, step))
}

func filterAction(ctx context.Context, filter string) (bool, error) {
	if strings.TrimSpace(filter) == "" {
		return false, nil
	}
	switch filter {

	case actionFilterSkip:
		return true, nil

	case actionFilterTimeout:
		// TODO: We don't properly store timed out action status.

	default:
		if proceed, err := strconv.ParseBool(filter); err != nil {
			return false, ctx.Oops().Errorf("invalid action filter result (%s) must be one of 'true', 'false', 'skip'", filter)
		} else if !proceed {
			return true, nil
		}
	}

	return false, nil
}
