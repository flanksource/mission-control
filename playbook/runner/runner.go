package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
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

	var previouslyRanAction models.PlaybookRunAction
	if err := ctx.DB().Model(&models.PlaybookRunAction{}).
		Where("playbook_run_id = ?", run.ID).
		Where("status IN ?", models.PlaybookActionFinalStates).
		Order("start_time DESC").
		First(&previouslyRanAction).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ctx.Oops("db").Wrap(err)
		}
	}

	if previouslyRanAction.Name == "" { // Nothing has run yet
		ctx.Logger.V(4).Infof("no previous action, running first action")
		return &playbookSpec.Actions[0], nil, nil
	}

	for i, action := range playbookSpec.Actions {
		isLastAction := i == len(playbookSpec.Actions)-1
		previousActionFailed := previouslyRanAction.Status == models.PlaybookActionStatusFailed

		if action.Name == previouslyRanAction.Name && previousActionFailed {
			canRetry := action.Retry != nil && previouslyRanAction.RetryCount < action.Retry.Limit
			if canRetry {
				return &playbookSpec.Actions[i], &previouslyRanAction, nil
			}
		}

		if isLastAction {
			return nil, nil, nil
		}

		if action.Name == previouslyRanAction.Name {
			// If last action failed do not run more steps unless it has a filter
			if previousActionFailed {
				alwaysAction := findNextActionWithFilter(playbookSpec.Actions[i+1:])
				return alwaysAction, &previouslyRanAction, nil
			}

			return &playbookSpec.Actions[i+1], &previouslyRanAction, nil
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
	if action.Delay == "" || run.Status == models.PlaybookRunStatusSleeping {
		return delay, nil
	}

	// We try to parse it directly as a duration first
	if d, err := duration.ParseDuration(action.Delay); err == nil {
		return time.Duration(d), nil
	}

	templateEnv, err := CreateTemplateEnv(ctx, &playbook, run, lastRan)
	if err != nil {
		return 0, ctx.Oops().Wrapf(err, "failed to template action")
	}

	oops := ctx.Oops().Hint(templateEnv.JSON(ctx))
	action.Delay, err = ctx.RunTemplate(gomplate.Template{Expression: action.Delay}, templateEnv.AsMap(ctx))
	if err != nil {
		return 0, oops.Wrapf(err, "failed to template action")
	}

	delay, err = action.DelayDuration()
	if err != nil {
		return 0, oops.Wrapf(err, "invalid duration (%s)", action.Delay)
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

func saveAIResultToSendHistory(ctx context.Context, run models.PlaybookRun) error {
	if run.NotificationSendID == nil {
		return nil
	}

	var playbookSpec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &playbookSpec); err != nil {
		return ctx.Oops().Wrap(err)
	}

	completedActions, err := run.GetActions(ctx.DB())
	if err != nil {
		return ctx.Oops().Wrap(err)
	}

	var aiActionNames, notificationActionNames []string
	for _, action := range playbookSpec.Actions {
		if action.AI != nil {
			aiActionNames = append(aiActionNames, action.Name)
		}
		if action.Notification != nil {
			notificationActionNames = append(notificationActionNames, action.Name)
		}
	}

	sendHistoryUpdate := models.NotificationSendHistory{
		ID: *run.NotificationSendID,
	}
	for _, action := range completedActions {
		if action.Status != models.PlaybookActionStatusCompleted || action.Result == nil {
			continue
		}

		if lo.Contains(aiActionNames, action.Name) {
			if diagnosisReport, ok := action.Result["json"].(string); ok {
				var aiDiagnosisReport map[string]string
				if err := json.Unmarshal([]byte(diagnosisReport), &aiDiagnosisReport); err != nil {
					return ctx.Oops().Wrap(err)
				}

				if headline, ok := aiDiagnosisReport["headline"]; ok {
					sendHistoryUpdate.ResourceHealthDescription = headline
				}
			}
		}

		if lo.Contains(notificationActionNames, action.Name) {
			if slackMsg, ok := action.Result["slack"].(string); ok {
				sendHistoryUpdate.Body = &slackMsg
			} else if body, ok := action.Result["body"].(string); ok {
				sendHistoryUpdate.Body = &body
			}
		}
	}

	if len(sendHistoryUpdate.ResourceHealthDescription) > 0 {
		if err := ctx.DB().Updates(sendHistoryUpdate).Error; err != nil {
			return ctx.Oops().Wrap(err)
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

	// Override the current spec with the run's spec
	playbook.Spec = run.Spec

	ctx = ctx.WithObject(run)

	action, lastRan, err := GetNextActionToRun(ctx, run)
	if err != nil {
		ctx.Tracef("Unable to get next action")
		return ctx.Oops().Wrap(err)
	}
	if action == nil {
		if err := run.End(ctx.DB()); err != nil {
			return ctx.Oops("db").Wrap(err)
		}

		// Callbacks once a run completes
		if err := saveAIResultToSendHistory(ctx, run); err != nil {
			// NOTE: we dont' want this error to cause a retry
			// Maybe, at some point we can have a callback registry that are tried in a separate cycle
			// and are retried on failure.
			ctx.Errorf("failed to save AI diagnosis to send history: %v", err)
		}

		return nil
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

func ExecuteAndSaveAction(ctx context.Context, playbookID any, action *models.PlaybookRunAction, actionSpec v1.PlaybookAction, templateEnv actions.TemplateEnv) error {
	db := ctx.DB()

	if err := action.Start(db); err != nil {
		return ctx.Oops().Wrap(err)
	}

	result, err := executeAction(ctx, playbookID, action.PlaybookRunID, *action, actionSpec, templateEnv)
	if err != nil {
		ctx.Errorf("action failed %+v", err)
		if err := action.Fail(db, result.data, err); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	} else if result.skipped {
		if err := action.Skip(db); err != nil {
			return ctx.Oops("db").Wrap(err)
		}
	} else if accessor, ok := result.data.(StatusAccessor); ok {
		switch accessor.GetStatus() {
		case models.PlaybookActionStatusFailed:
			ctx.Warnf("action returned failure\n%v", logger.Pretty(result.data))
			if err := action.Fail(db, result.data, nil); err != nil {
				return ctx.Oops("db").Wrap(err)
			}

		case models.PlaybookActionStatusWaitingChildren:
			ctx.Tracef("action is awaiting children\n%v", logger.Pretty(result.data))
			if err := action.WaitForChildren(db); err != nil {
				return ctx.Oops("db").Wrap(err)
			}

		case models.PlaybookActionStatusCompleted:
			ctx.Tracef("action completed\n%v", logger.Pretty(result.data))
			if err := action.Complete(db, result.data); err != nil {
				return ctx.Oops("db").Wrap(err)
			}
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
		return ctx.Oops().Wrapf(err, "failed to unmarshal playbook spec")
	}

	ctx = ctx.WithObject(action, run).WithSubject(playbook.ID.String())

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
	step.EnforceTimeoutLimit(ctx, spec)

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

	if step.AI != nil && step.AI.Config == "" && run.ConfigID != nil {
		step.AI.Config = run.ConfigID.String()
	}

	return oops.Wrap(ExecuteAndSaveAction(ctx, run.PlaybookID, action, step, templateEnv))
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
