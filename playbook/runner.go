package playbook

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// hostRunner is the name of the host runner
const hostRunner = "local"

func getNextActionToRun(ctx context.Context, playbook models.Playbook, runID uuid.UUID) (*v1.PlaybookAction, error) {
	var playbookSpec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &playbookSpec); err != nil {
		return nil, err
	}

	var lastRanAction models.PlaybookRunAction
	if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("playbook_run_id = ?", runID).Where("status IN ?", models.PlaybookActionFinalStates).Order("start_time DESC").First(&lastRanAction).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	for i, action := range playbookSpec.Actions {
		if lastRanAction.Name == "" { // Nothing has run yet
			return &playbookSpec.Actions[i], nil
		}

		if action.Name != lastRanAction.Name {
			continue
		}

		if i == len(playbookSpec.Actions)-1 {
			break // All the actions have run
		}

		return &playbookSpec.Actions[i+1], nil
	}

	return nil, nil
}

type ActionForAgent struct {
	Run         *models.PlaybookRun       `json:"run,omitempty"`
	ActionSpec  *v1.PlaybookAction        `json:"action_spec,omitempty"`
	Action      *models.PlaybookRunAction `json:"action,omitempty"`
	TemplateEnv *actions.TemplateEnv      `json:"template_env"`
}

func GetActionForAgent(ctx context.Context, agent *models.Agent) (*ActionForAgent, error) {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())

	query := `
		SELECT playbook_runs.*
		FROM playbook_runs
		INNER JOIN playbooks ON playbooks.id = playbook_runs.playbook_id
		WHERE status IN (?, ?)
			AND scheduled_time <= NOW()
			AND playbooks.spec->'runsOn' @> ?
		ORDER BY scheduled_time
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var runs []models.PlaybookRun
	if err := ctx.DB().Raw(query, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusSleeping, fmt.Sprintf(`["%s"]`, agent.Name)).Find(&runs).Error; err != nil {
		return nil, err
	}

	if len(runs) == 0 {
		return &ActionForAgent{}, nil
	}
	run := runs[0]

	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbook).Error; err != nil {
		return nil, err
	}

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(map[string]any{"status": models.PlaybookRunStatusRunning}).Error; err != nil {
		return nil, fmt.Errorf("failed to update playbook run status: %w", err)
	}

	actionToRun, err := getNextActionToRun(ctx, playbook, run.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next action to run: %w", err)
	}

	if actionToRun == nil {
		return &ActionForAgent{}, nil
	}

	newAction := models.PlaybookRunAction{
		PlaybookRunID: run.ID,
		Name:          actionToRun.Name,
		Status:        models.PlaybookActionStatusRunning,
		AgentID:       lo.ToPtr(agent.ID),
	}
	if err := ctx.DB().Create(&newAction).Error; err != nil {
		return nil, fmt.Errorf("failed to create a new playbook run: %w", err)
	}

	templateEnv, err := prepareTemplateEnv(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare template env: %w", err)
	}

	if err := templateAction(ctx, run, newAction, actionToRun, templateEnv); err != nil {
		return nil, fmt.Errorf("failed to template action: %w", err)
	}

	output := ActionForAgent{
		Action:      &newAction,
		Run:         &run,
		ActionSpec:  actionToRun,
		TemplateEnv: &templateEnv,
	}

	return &output, tx.Commit().Error
}

// HandleRun finds the next action that this host should run.
// In case it doesn't find any, it marks the run as waiting.
func HandleRun(ctx context.Context, run models.PlaybookRun) error {
	ctx, span := ctx.StartSpan("HandleRun")
	defer span.End()

	var playbook models.Playbook
	if err := ctx.DB().First(&playbook, run.PlaybookID).Error; err != nil {
		return fmt.Errorf("failed to fetch playbook: %w", err)
	}

	action, err := getNextActionToRun(ctx, playbook, run.ID)
	if err != nil {
		return fmt.Errorf("failed to get next action to run: %w", err)
	} else if action == nil {
		// All the actions have run
		updateColumns := map[string]any{
			"status":   models.PlaybookRunStatusCompleted,
			"end_time": gorm.Expr("CLOCK_TIMESTAMP()"),
		}
		return ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(updateColumns).Error
	}

	runUpdates := map[string]any{
		"start_time": gorm.Expr("CASE WHEN start_time IS NULL THEN CLOCK_TIMESTAMP() ELSE start_time END"),
	}
	if len(action.RunsOn) == 0 || lo.Contains(action.RunsOn, hostRunner) {
		runAction := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          action.Name,
			Status:        models.PlaybookActionStatusScheduled,
		}
		if err := ctx.DB().Save(&runAction).Error; err != nil {
			return fmt.Errorf("failed to save run action: %w", err)
		}

		runUpdates["status"] = models.PlaybookRunStatusRunning
	} else {
		// The host cannot run the next action.
		// Mark the run as waiting and let another runner pick up the action.
		runUpdates["status"] = models.PlaybookRunStatusWaiting
	}

	return ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(runUpdates).Error
}

func ExecuteRunOld(ctx context.Context, run models.PlaybookRun) {
	ctx, span := ctx.StartSpan("ExecuteRunOld")
	defer span.End()

	var runOptions runExecOptions
	if run.Status == models.PlaybookRunStatusSleeping {
		// We fetch the action that's currently sleeping for this playbook run,
		// and begin the execution from there.
		var sleepingAction models.PlaybookRunAction
		if err := ctx.DB().Where("playbook_run_id = ?", run.ID).Where("status = ?", models.PlaybookRunStatusSleeping).Find(&sleepingAction).Error; err != nil {
			logger.Errorf("failed to fetch actions: %v", err)
			return
		}

		runOptions.StartFrom = &sleepingAction
	}

	{
		columnUpdates := map[string]any{
			"status": models.PlaybookRunStatusRunning,
		}

		if run.StartTime == nil || run.StartTime.IsZero() {
			columnUpdates["start_time"] = gorm.Expr("CLOCK_TIMESTAMP()")
		}

		if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(columnUpdates).Error; err != nil {
			logger.Errorf("failed to update playbook run status: %v", err)
			return
		}
	}

	var columnUpdates = map[string]any{}
	if runResponse, err := executeRun(ctx, run, runOptions); err != nil {
		logger.Errorf("failed to execute playbook run: %v", err)
		columnUpdates["status"] = models.PlaybookRunStatusFailed
		columnUpdates["end_time"] = gorm.Expr("CLOCK_TIMESTAMP()")
	} else if runResponse.Sleep > 0 {
		columnUpdates["scheduled_time"] = gorm.Expr(fmt.Sprintf("CLOCK_TIMESTAMP() + INTERVAL '%d SECONDS'", int(runResponse.Sleep.Seconds())))
		columnUpdates["status"] = models.PlaybookRunStatusSleeping
	} else {
		columnUpdates["status"] = models.PlaybookRunStatusCompleted
		columnUpdates["end_time"] = gorm.Expr("CLOCK_TIMESTAMP()")
	}

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
	}
}

type runExecOptions struct {
	StartFrom *models.PlaybookRunAction
}

type runExecutionResult struct {
	// Sleep, when set, indicates that the run execution should pause and continue
	// after the specified duration.
	Sleep time.Duration
}

func prepareTemplateEnv(ctx context.Context, run models.PlaybookRun) (actions.TemplateEnv, error) {
	templateEnv := actions.TemplateEnv{
		Params: run.Parameters,
	}

	if run.ComponentID != nil {
		if err := ctx.DB().Where("id = ?", run.ComponentID).First(&templateEnv.Component).Error; err != nil {
			return templateEnv, fmt.Errorf("failed to fetch component: %w", err)
		}
	} else if run.ConfigID != nil {
		if err := ctx.DB().Where("id = ?", run.ConfigID).First(&templateEnv.Config).Error; err != nil {
			return templateEnv, fmt.Errorf("failed to fetch config: %w", err)
		}
	} else if run.CheckID != nil {
		if err := ctx.DB().Where("id = ?", run.CheckID).First(&templateEnv.Check).Error; err != nil {
			return templateEnv, fmt.Errorf("failed to fetch check: %w", err)
		}
	}

	return templateEnv, nil
}

// executeRun executes all the actions in the given playbook one at a time in order.
func executeRun(ctx context.Context, run models.PlaybookRun, opt runExecOptions) (*runExecutionResult, error) {
	var playbookModel models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbookModel).Error; err != nil {
		return nil, err
	}

	playbook, err := v1.PlaybookFromModel(playbookModel)
	if err != nil {
		return nil, err
	}

	logger.WithValues("playbook", playbook.Name).
		WithValues("parameters", run.Parameters).
		WithValues("config", run.ConfigID).
		WithValues("check", run.CheckID).
		WithValues("component", run.ComponentID).
		Infof("Executing playbook run: %s", run.ID)

	templateEnv, err := prepareTemplateEnv(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare template env: %w", err)
	}

	var continueFromAction string
	if opt.StartFrom != nil {
		continueFromAction = opt.StartFrom.Name
	}

	for _, actionSpec := range playbook.Spec.Actions {
		if continueFromAction != "" && actionSpec.Name != continueFromAction {
			continue
		}

		runAction := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          actionSpec.Name,
			Status:        models.PlaybookActionStatusRunning,
		}
		if opt.StartFrom != nil {
			runAction = *opt.StartFrom
			if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", runAction.ID).UpdateColumns(map[string]any{
				"status":     models.PlaybookActionStatusRunning,
				"start_time": gorm.Expr("CLOCK_TIMESTAMP()"),
			}).Error; err != nil {
				return nil, fmt.Errorf("failed to update playbook run action status to %s: %w", models.PlaybookActionStatusRunning, err)
			}
		} else {
			if err := ctx.DB().Save(&runAction).Error; err != nil {
				return nil, fmt.Errorf("failed to create playbook run action: %w", err)
			}
		}

		if duration, err := actionSpec.DelayDuration(templateEnv.AsMap()); err != nil {
			return nil, err
		} else if duration > 0 && actionSpec.Name != continueFromAction {
			logger.Debugf("Pausing run execution. Sleeping for %v", duration)

			if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", runAction.ID).UpdateColumns(map[string]any{
				"status":     models.PlaybookActionStatusSleeping,
				"start_time": gorm.Expr("NULL"),
			}).Error; err != nil {
				return nil, fmt.Errorf("failed to update playbook run action status to %s: %w", models.PlaybookActionStatusSleeping, err)
			}

			return &runExecutionResult{
				Sleep: duration,
			}, nil
		} else if actionSpec.Name == continueFromAction {
			continueFromAction = ""
		}

		columnUpdates := map[string]any{
			"end_time": gorm.Expr("CLOCK_TIMESTAMP()"),
		}

		if err := templateAction(ctx, run, runAction, &actionSpec, templateEnv); err != nil {
			return nil, fmt.Errorf("failed to template action: %w", err)
		}

		result, err := ExecuteAction(ctx, run, runAction, actionSpec, templateEnv)
		if err != nil {
			logger.Errorf("failed to execute action: %v", err)
			columnUpdates["status"] = models.PlaybookActionStatusFailed
			columnUpdates["error"] = err.Error()
		} else if result.Skipped {
			columnUpdates["status"] = models.PlaybookActionStatusSkipped
		} else {
			columnUpdates["status"] = models.PlaybookActionStatusCompleted
			columnUpdates["result"] = result.Data
		}

		if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", runAction.ID).UpdateColumns(&columnUpdates).Error; err != nil {
			return nil, fmt.Errorf("failed to update playbook action result: %w", err)
		}
	}

	return &runExecutionResult{}, nil
}

// ExecuteActionResult is the result of executing an action
type ExecuteActionResult struct {
	// result of the action as JSON
	Data []byte

	// Skipped is true if the action was Skipped by the action filter
	Skipped bool
}

func templateAction(ctx context.Context, run models.PlaybookRun, runAction models.PlaybookRunAction, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
	if actionSpec.Filter != "" {
		gomplateTemplate := gomplate.Template{
			Expression: actionSpec.Filter,
			CelEnvs:    getActionCelEnvs(ctx, run.ID.String(), runAction.ID.String()),
			Functions:  actionFilterFuncs,
		}
		var err error
		if actionSpec.Filter, err = gomplate.RunTemplate(env.AsMap(), gomplateTemplate); err != nil {
			return fmt.Errorf("failed to parse action filter (%s): %w", actionSpec.Filter, err)
		}
	}

	templateFuncs := map[string]any{
		"getLastAction": func() any {
			r, err := GetLastAction(ctx, run.ID.String(), runAction.ID.String())
			if err != nil {
				logger.Errorf("failed to get last action for run(%s): %v", run.ID, err)
				return ""
			}

			return r
		},
		"getAction": func(actionName string) any {
			r, err := GetActionByName(ctx, run.ID.String(), actionName)
			if err != nil {
				logger.Errorf("failed to get action(%s) for run(%s): %v", actionName, run.ID, err)
				return ""
			}

			return r
		},
	}

	templater := gomplate.StructTemplater{
		Values:         env.AsMap(),
		ValueFunctions: true,
		RequiredTag:    "template",
		DelimSets: []gomplate.Delims{
			{Left: "{{", Right: "}}"},
			{Left: "$(", Right: ")"},
		},
		Funcs: collections.MergeMap(templateFuncs, actionFilterFuncs),
	}
	return templater.Walk(&actionSpec)
}

func ExecuteAction(ctx context.Context, run models.PlaybookRun, runAction models.PlaybookRunAction, actionSpec v1.PlaybookAction, env actions.TemplateEnv) (*ExecuteActionResult, error) {
	ctx, span := ctx.StartSpan("executeAction")
	defer span.End()

	logger.WithValues("runID", run.ID).Infof("Executing action: %s", actionSpec.Name)

	if timeout, _ := actionSpec.TimeoutDuration(); timeout > 0 {
		var cancel gocontext.CancelFunc
		ctx, cancel = ctx.WithTimeout(timeout)
		defer cancel()
	}

	if actionSpec.Filter != "" {
		switch actionSpec.Filter {
		case actionFilterAlways:
			// Do nothing, just run the action

		case actionFilterSkip:
			return &ExecuteActionResult{Skipped: true}, nil

		case actionFilterFailure:
			if count, err := db.GetPlaybookActionsForStatus(ctx, run.ID, models.PlaybookActionStatusFailed); err != nil {
				return nil, fmt.Errorf("failed to get playbook actions for status(%s): %w", models.PlaybookActionStatusFailed, err)
			} else if count == 0 {
				return &ExecuteActionResult{Skipped: true}, nil
			}

		case actionFilterSuccess:
			if count, err := db.GetPlaybookActionsForStatus(ctx, run.ID, models.PlaybookActionStatusFailed); err != nil {
				return nil, fmt.Errorf("failed to get playbook actions for status(%s): %w", models.PlaybookActionStatusFailed, err)
			} else if count > 0 {
				return &ExecuteActionResult{Skipped: true}, nil
			}

		case actionFilterTimeout:
			// TODO: We don't properly store timed out action status.

		default:
			if proceed, err := strconv.ParseBool(actionSpec.Filter); err != nil {
				return nil, fmt.Errorf("action filter didn't evaluate to a boolean value (%s) neither returned any special command", actionSpec.Filter)
			} else if !proceed {
				return &ExecuteActionResult{Skipped: true}, nil
			}
		}
	}

	if actionSpec.Exec != nil {
		var e actions.ExecAction
		res, err := e.Run(ctx, *actionSpec.Exec, env)
		if err != nil {
			return nil, err
		}

		if err := saveArtifacts(ctx, run.ID, res.Artifacts); err != nil {
			return nil, fmt.Errorf("error saving artifacts: %v", err)
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &ExecuteActionResult{Data: jsonData}, nil
	}

	if actionSpec.HTTP != nil {
		var e actions.HTTP
		res, err := e.Run(ctx, *actionSpec.HTTP, env)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &ExecuteActionResult{Data: jsonData}, nil
	}

	if actionSpec.SQL != nil {
		var e actions.SQL
		res, err := e.Run(ctx, *actionSpec.SQL, env)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &ExecuteActionResult{Data: jsonData}, nil
	}

	if actionSpec.Pod != nil {
		e := actions.Pod{
			PlaybookRun: run,
		}

		timeout, _ := actionSpec.TimeoutDuration()
		res, err := e.Run(ctx, *actionSpec.Pod, env, timeout)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &ExecuteActionResult{Data: jsonData}, nil
	}

	if actionSpec.GitOps != nil {
		var e actions.GitOps
		res, err := e.Run(ctx, *actionSpec.GitOps, env)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &ExecuteActionResult{Data: jsonData}, nil
	}

	if actionSpec.Notification != nil {
		var e actions.Notification
		err := e.Run(ctx, *actionSpec.Notification, env)
		if err != nil {
			return nil, err
		}

		return &ExecuteActionResult{Data: []byte("{}")}, nil
	}

	return &ExecuteActionResult{}, nil
}
