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
	"github.com/flanksource/duty/schema/openapi"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/uuid"
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

func GetNextActionToRun(ctx context.Context, playbook models.Playbook, run models.PlaybookRun) (action *v1.PlaybookAction, err error) {
	ctx.Logger.V(3).Infof("Getting next action to run for playbook %s run %s", playbook.Name, run.ID)
	ctx = ctx.WithObject(playbook, run)
	if validationErr, err := openapi.ValidatePlaybookSpec(playbook.Spec); err != nil {
		return nil, err
	} else if validationErr != nil {
		return nil, validationErr
	}

	var playbookSpec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &playbookSpec); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	var lastRanAction models.PlaybookRunAction
	if err := ctx.DB().Model(&models.PlaybookRunAction{}).
		Where("playbook_run_id = ?", run.ID).
		Where("status IN ?", models.PlaybookActionFinalStates).
		Order("start_time DESC").
		First(&lastRanAction).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ctx.Oops("db").Wrap(err)
		}
	}

	if lastRanAction.Name == "" { // Nothing has run yet
		ctx.Logger.V(4).Infof("no previous action, running first action")
		return &playbookSpec.Actions[0], nil
	}

	for i, action := range playbookSpec.Actions {
		if i == len(playbookSpec.Actions)-1 {
			return nil, nil
		}

		if action.Name == lastRanAction.Name {
			// If last action failed do not run more steps unless it has a filter
			if lastRanAction.Status == models.PlaybookActionStatusFailed {
				alwaysAction := findNextActionWithFilter(playbookSpec.Actions[i+1:])
				return alwaysAction, nil
			}
			return &playbookSpec.Actions[i+1], nil
		}
	}

	return nil, ctx.Oops("db").Errorf("could not find action to run or complete")
}

func findNextActionWithFilter(actions []v1.PlaybookAction) *v1.PlaybookAction {
	for _, action := range actions {
		if action.Filter != "" {
			return &action
		}
	}
	return nil
}

func getActionSpec(ctx context.Context, playbook *models.Playbook, name string) (*v1.PlaybookAction, error) {
	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return nil, err
	}
	for _, action := range spec.Actions {
		if action.Name == name {
			action.PlaybookID = playbook.ID.String()
			return &action, nil
		}
	}

	return nil, nil
}

func CheckPlaybookFilter(ctx context.Context, playbookSpec v1.PlaybookSpec, templateEnv actions.TemplateEnv) error {
	for _, f := range playbookSpec.Filters {
		val, err := ctx.RunTemplate(gomplate.Template{Expression: f}, templateEnv.AsMap())
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

// ScheduleRun finds the next action step that needs to run and
// creates the PlaybookActionRun in a scheduled status, with an optional agentId
func ScheduleRun(ctx context.Context, run models.PlaybookRun) error {

	var playbook models.Playbook

	if err := ctx.DB().First(&playbook, run.PlaybookID).Error; err != nil {
		return ctx.Oops("db").Wrap(err)
	}

	ctx = ctx.WithObject(playbook, run)

	action, err := GetNextActionToRun(ctx, playbook, run)
	if err != nil {
		ctx.Tracef("Unable to get next action")
		return ctx.Oops().Wrap(err)
	}
	if action == nil {
		return ctx.Oops("db").Wrap(run.End(ctx.DB()))
	}

	ctx = ctx.WithObject(playbook, action, run)

	// The delays on the action should be applied here & action consumers do not run the delay.
	var delay time.Duration
	if action.Delay != "" && run.Status != models.PlaybookRunStatusSleeping {
		templateEnv, err := CreateTemplateEnv(ctx, &playbook, &run)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to template action")
		}
		oops := ctx.Oops().Hint(templateEnv.String())
		if action.Delay, err = ctx.RunTemplate(gomplate.Template{Expression: action.Delay}, templateEnv.AsMap()); err != nil {
			return oops.Wrapf(err, "failed to template action")
		} else if delay, err = action.DelayDuration(); err != nil {
			return oops.Wrapf(err, "invalid duration n (%s)", action.Delay)
		}
	}

	if delay > 0 {
		ctx.Tracef("delaying %s by %s", action.Name, delay)
		// Defer the scheduling of this run,
		return ctx.Oops().Wrap(run.Delay(ctx.DB(), delay))
	}

	if len(action.RunsOn) == 0 || lo.Contains(action.RunsOn, Main) {
		if runAction, err := run.StartAction(ctx.DB(), action.Name); err != nil {
			return ctx.Oops("db").Wrap(err)
		} else {
			ctx.Tracef("started %s (%v) on local", action.Name, runAction.ID)
		}
	}

	if agent, err := db.FindFirstAgent(ctx, action.RunsOn...); err != nil {
		return ctx.Oops("db").Wrap(err)
	} else if agent == nil {
		return ctx.Oops("db").Wrapf(err, "failed to find any agent (%s)", strings.Join(action.RunsOn, ","))
	} else {
		// Assign the action to an agent and step the status to Waiting
		// When the agent polls for new actions to run, we return and then set the status to Running
		ctx.Tracef("assigning %s to agent %s", action.Name, agent.Name)
		return ctx.Oops("db").Wrap(run.Assign(ctx.DB(), agent, action.Name))
	}
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
	} else {
		ctx.Tracef("action completed\n%v", logger.Pretty(result.data))
		if err := action.Complete(db, result.data); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	}

	return nil

}

// TemplateAndExecuteAction executes the given playbook action after templating it.
func RunAction(ctx context.Context, run *models.PlaybookRun, action *models.PlaybookRunAction) error {
	playbook, err := action.GetPlaybook(ctx.DB())
	if err != nil {
		return err
	}
	if playbook == nil {
		return ctx.Oops().Errorf("playbook not found")
	}

	spec, err := v1.PlaybookFromModel(*playbook)
	if err != nil {
		return err
	}
	ctx = ctx.WithObject(playbook, action, run)
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
func TemplateAndExecuteAction(ctx context.Context, spec v1.Playbook, playbook *models.Playbook, run *models.PlaybookRun, action *models.PlaybookRunAction) error {
	ctx = ctx.WithObject(playbook, run, action)

	step, found := lo.Find(spec.Spec.Actions, func(i v1.PlaybookAction) bool { return i.Name == action.Name })
	if !found {
		return ctx.Oops().Errorf("action '%s' not found", action.Name)
	}

	templateEnv, err := CreateTemplateEnv(ctx, playbook, run)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to create template env")
	}

	oops := ctx.Oops().Hint(templateEnv.String())

	for _, e := range spec.Spec.Env {
		val, err := ctx.GetEnvValueFromCache(e, ctx.GetNamespace())
		if err != nil {
			return ctx.Oops("env").Wrapf(err, "failed to get %s", e.Name)
		} else {
			templateEnv.Env[e.Name] = val
		}
	}

	if err := templateActionExpressions(ctx, run, action, &step, templateEnv); err != nil {
		return oops.Wrapf(err, "failed to template expressions")
	}

	if err := TemplateAction(ctx, run, action, &step, templateEnv); err != nil {
		return oops.Wrapf(err, "failed to template")
	}

	return oops.Wrap(ExecuteAndSaveAction(ctx, run.PlaybookID, action, step))
}

func filterAction(ctx context.Context, runID uuid.UUID, filter string) (bool, error) {
	switch filter {
	case actionFilterAlways, "":
		return false, nil

	case actionFilterSkip:
		return true, nil

	case actionFilterFailure:
		if count, err := db.GetPlaybookActionsForStatus(ctx, runID, models.PlaybookActionStatusFailed); err != nil {
			return false, ctx.Oops("db").Wrap(err)
		} else if count == 0 {
			return true, nil
		}

	case actionFilterSuccess:
		if count, err := db.GetPlaybookActionsForStatus(ctx, runID, models.PlaybookActionStatusFailed); err != nil {
			return false, ctx.Oops("db").Wrap(err)
		} else if count > 0 {
			return true, nil
		}

	case actionFilterTimeout:
		// TODO: We don't properly store timed out action status.

	default:
		if proceed, err := strconv.ParseBool(filter); err != nil {
			return false, ctx.Oops().Errorf("action filter didn't evaluate to a boolean value (%s) neither returned any special command", filter)
		} else if !proceed {
			return true, nil
		}
	}

	return false, nil
}
