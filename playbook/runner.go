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
	"github.com/flanksource/duty/schema/openapi"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

const (
	// runnerMain is the identifier for the upstream server.
	runnerMain = "local"

	// runnerAgent is the identifier for any agent runner.
	runnerAgent = "agent"
)

func getNextActionToRun(ctx context.Context, playbook models.Playbook, runID uuid.UUID) (*v1.PlaybookAction, error) {
	if validationErr, err := openapi.ValidatePlaybookSpec(playbook.Spec); err != nil {
		return nil, err
	} else if validationErr != nil {
		return nil, validationErr
	}

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

	if lastRanAction.Name == "" { // Nothing has run yet
		return &playbookSpec.Actions[0], nil
	}

	for i, action := range playbookSpec.Actions {
		if i == len(playbookSpec.Actions)-1 {
			break // All the actions have run
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

	return nil, nil
}

func findNextActionWithFilter(actions []v1.PlaybookAction) *v1.PlaybookAction {
	for _, action := range actions {
		if action.Filter != "" {
			return &action
		}
	}
	return nil
}

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
	if err := ctx.DB().Raw(query, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusWaiting, fmt.Sprintf(`["%s"]`, agent.Name)).Find(&runs).Error; err != nil {
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
		Status:        models.PlaybookActionStatusScheduled,
		AgentID:       lo.ToPtr(agent.ID),
	}
	if err := ctx.DB().Create(&newAction).Error; err != nil {
		return nil, fmt.Errorf("failed to create a new playbook run: %w", err)
	}

	templateEnv, err := prepareTemplateEnv(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare template env: %w", err)
	}

	if err := templateActionExpressions(ctx, run, newAction, actionToRun, templateEnv); err != nil {
		return nil, fmt.Errorf("failed to template action: %w", err)
	}

	if actionToRun.TemplatesOn == "" || actionToRun.TemplatesOn == runnerMain {
		if err := templateAction(ctx, run, newAction, actionToRun, templateEnv); err != nil {
			return nil, fmt.Errorf("failed to template action: %w", err)
		}
	}

	output := ActionForAgent{
		Action:      newAction,
		Run:         run,
		ActionSpec:  *actionToRun,
		TemplateEnv: templateEnv,
	}

	if skip, err := filterAction(ctx, newAction.ID, actionToRun.Filter); err != nil {
		return nil, fmt.Errorf("failed to evaluate action filter: %w", err)
	} else {
		// We run the filter on the upstream and simply send the filter result to the agent.
		actionToRun.Filter = strconv.FormatBool(!skip)
	}

	return &output, tx.Commit().Error
}

func checkPlaybookFilter(ctx context.Context, playbookSpec v1.PlaybookSpec, templateEnv actions.TemplateEnv) error {
	for _, f := range playbookSpec.Filters {
		val, err := ctx.RunTemplate(gomplate.Template{Expression: f}, templateEnv.AsMap())
		if err != nil {
			return fmt.Errorf("invalid playbook filter [%s]: %s", f, err)

		}

		// The expression must return a boolean
		if val != "true" {
			return fmt.Errorf("%s", val)
		}
	}
	return nil
}

// HandleRun finds the next action that this host should run.
// In case it doesn't find any, it marks the run as waiting.
func HandleRun(ctx context.Context, run models.PlaybookRun) error {
	ctx, span := ctx.StartSpan("HandleRun")
	defer span.End()

	var playbook models.Playbook
	if err := ctx.DB().First(&playbook, run.PlaybookID).Error; err != nil {
		return fmt.Errorf("failed to fetch playbook(%s): %w", run.PlaybookID, err)
	}

	action, err := getNextActionToRun(ctx, playbook, run.ID)
	if err != nil {
		return fmt.Errorf("failed to get next action to run: %w", err)
	} else if action == nil {
		// All the actions have run
		actionStatuses, err := db.GetActionStatuses(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("failed to get action statuses for run(%s): %w", run.ID, err)
		}

		updateColumns := map[string]any{
			"end_time": gorm.Expr("CLOCK_TIMESTAMP()"),
			"status":   evaluateRunStatus(actionStatuses),
		}

		return ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(updateColumns).Error
	}

	runUpdates := map[string]any{
		"start_time": gorm.Expr("CASE WHEN start_time IS NULL THEN CLOCK_TIMESTAMP() ELSE start_time END"),
	}

	// The delays on the action should be applied here & action consumers do not run the delay.
	var delay time.Duration
	if action.Delay != "" && run.Status != models.PlaybookRunStatusSleeping {
		templateEnv, err := prepareTemplateEnv(ctx, run)
		if err != nil {
			return fmt.Errorf("failed to prepare template env for run(%s): %w", run.ID, err)
		}

		gomplateTemplate := gomplate.Template{Expression: action.Delay}
		if action.Delay, err = ctx.RunTemplate(gomplateTemplate, templateEnv.AsMap()); err != nil {
			return fmt.Errorf("failed to parse action delay (%s): %w", action.Delay, err)
		} else if delay, err = action.DelayDuration(); err != nil {
			return fmt.Errorf("failed to parse action delay as a duration (%s): %w", action.Delay, err)
		}
	}

	if delay > 0 {
		// The host shouldn't create the action if there's a delay.
		// Rather, defer the scheduling of the run & then the action will be created when the delay is over.
		runUpdates["scheduled_time"] = gorm.Expr(fmt.Sprintf("CLOCK_TIMESTAMP() + INTERVAL '%d SECONDS'", int(delay.Seconds())))
		runUpdates["status"] = models.PlaybookRunStatusSleeping
	} else {
		canRunOnHost := len(action.RunsOn) == 0 || lo.Contains(action.RunsOn, runnerMain)
		if !canRunOnHost {
			// Simply, ark the run as waiting and let another runner pick up the action.
			runUpdates["status"] = models.PlaybookRunStatusWaiting
		} else {
			runAction := models.PlaybookRunAction{
				PlaybookRunID: run.ID,
				Name:          action.Name,
				Status:        models.PlaybookActionStatusScheduled,
			}
			if err := ctx.DB().Save(&runAction).Error; err != nil {
				return fmt.Errorf("failed to save run action: %w", err)
			}

			runUpdates["status"] = models.PlaybookRunStatusRunning
		}
	}

	return ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(runUpdates).Error
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

func executeAndSaveAction(ctx context.Context, playbookID, runID uuid.UUID, actionToRun models.PlaybookRunAction, actionSpec v1.PlaybookAction) error {
	// Log the start time
	columnUpdates := map[string]any{
		"start_time": gorm.Expr("CASE WHEN start_time IS NULL THEN CLOCK_TIMESTAMP() ELSE start_time END"),
		"status":     models.PlaybookActionStatusRunning,
	}
	if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", actionToRun.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		return fmt.Errorf("failed to update playbook action result: %w", err)
	}

	columnUpdates = map[string]any{}
	result, err := executeAction(ctx, playbookID, runID, actionToRun, actionSpec)
	if err != nil {
		logger.Errorf("failed to execute action: %v", err)
		columnUpdates["status"] = models.PlaybookActionStatusFailed
		columnUpdates["error"] = err.Error()
	} else if result.skipped {
		columnUpdates["status"] = models.PlaybookActionStatusSkipped
	} else {
		columnUpdates["status"] = models.PlaybookActionStatusCompleted
		columnUpdates["result"] = result.data
	}

	if lo.Contains(models.PlaybookActionFinalStates, columnUpdates["status"].(models.PlaybookActionStatus)) {
		columnUpdates["end_time"] = gorm.Expr("CLOCK_TIMESTAMP()")
	}

	if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", actionToRun.ID).UpdateColumns(columnUpdates).Error; err != nil {
		return fmt.Errorf("failed to update playbook action result: %w", err)
	}

	// If the action has reached its final state then re-schedule the run.
	// NOTE: Maybe this could be a db trigger.
	if lo.Contains(models.PlaybookActionFinalStates, columnUpdates["status"].(models.PlaybookActionStatus)) {
		runUpdates := map[string]any{
			"status":         models.PlaybookRunStatusScheduled,
			"scheduled_time": gorm.Expr("CLOCK_TIMESTAMP()"),
		}
		if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", actionToRun.PlaybookRunID).UpdateColumns(runUpdates).Error; err != nil {
			return err
		}
	}

	return nil
}

// templateAndExecuteAction executes the given playbook action after templating it.
func templateAndExecuteAction(ctx context.Context, envs []types.EnvVar, run models.PlaybookRun, actionToRun models.PlaybookRunAction, actionSpec v1.PlaybookAction) error {
	logger.WithValues("run.id", run.ID).WithValues("parameters", run.Parameters).
		WithValues("config", run.ConfigID).WithValues("check", run.CheckID).WithValues("component", run.ComponentID).
		Infof("Executing playbook action: %s", actionToRun.ID)

	templateEnv, err := prepareTemplateEnv(ctx, run)
	if err != nil {
		return fmt.Errorf("failed to prepare template env: %w", err)
	}

	templater := ctx.NewStructTemplater(templateEnv.AsMap(), "", nil)
	if err := templater.Walk(&envs); err != nil {
		return fmt.Errorf("failed to walk envs: %w", err)
	}

	templateEnv.Env = make(map[string]string, len(envs))
	for _, e := range envs {
		val, err := ctx.GetEnvValueFromCache(e)
		if err != nil {
			return fmt.Errorf("failed to get env value (%s): %w", e.Name, err)
		} else {
			templateEnv.Env[e.Name] = val
		}
	}

	if err := templateActionExpressions(ctx, run, actionToRun, &actionSpec, templateEnv); err != nil {
		return fmt.Errorf("failed to template expressions in the action: %w", err)
	}

	if err := templateAction(ctx, run, actionToRun, &actionSpec, templateEnv); err != nil {
		return fmt.Errorf("failed to template action: %w", err)
	}

	return executeAndSaveAction(ctx, run.PlaybookID, run.ID, actionToRun, actionSpec)
}

// executeActionResult is the result of executing an action
type executeActionResult struct {
	// result of the action as JSON
	data []byte

	// skipped is true if the action was skipped by the action filter
	skipped bool
}

// templateAction templatizes all the cel-expressions in the action
func templateActionExpressions(ctx context.Context, run models.PlaybookRun, runAction models.PlaybookRunAction, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
	if actionSpec.Filter != "" {
		gomplateTemplate := gomplate.Template{
			Expression: actionSpec.Filter,
			CelEnvs:    getActionCelEnvs(ctx, run.ID.String(), runAction.ID.String()),
			Functions:  actionFilterFuncs,
		}
		var err error
		if actionSpec.Filter, err = ctx.RunTemplate(gomplateTemplate, env.AsMap()); err != nil {
			return fmt.Errorf("failed to parse action filter (%s): %w", actionSpec.Filter, err)
		}
	}

	return nil
}

// templateAction templatizes all the go tempaltes in the action
func templateAction(ctx context.Context, run models.PlaybookRun, runAction models.PlaybookRunAction, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
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

	templater := ctx.NewStructTemplater(env.AsMap(), "template", collections.MergeMap(templateFuncs, actionFilterFuncs))
	return templater.Walk(&actionSpec)
}

func filterAction(ctx context.Context, runID uuid.UUID, filter string) (bool, error) {
	switch filter {
	case actionFilterAlways, "":
		return false, nil

	case actionFilterSkip:
		return true, nil

	case actionFilterFailure:
		if count, err := db.GetPlaybookActionsForStatus(ctx, runID, models.PlaybookActionStatusFailed); err != nil {
			return false, fmt.Errorf("failed to get playbook actions for status(%s): %w", models.PlaybookActionStatusFailed, err)
		} else if count == 0 {
			return true, nil
		}

	case actionFilterSuccess:
		if count, err := db.GetPlaybookActionsForStatus(ctx, runID, models.PlaybookActionStatusFailed); err != nil {
			return false, fmt.Errorf("failed to get playbook actions for status(%s): %w", models.PlaybookActionStatusFailed, err)
		} else if count > 0 {
			return true, nil
		}

	case actionFilterTimeout:
		// TODO: We don't properly store timed out action status.

	default:
		if proceed, err := strconv.ParseBool(filter); err != nil {
			return false, fmt.Errorf("action filter didn't evaluate to a boolean value (%s) neither returned any special command", filter)
		} else if !proceed {
			return true, nil
		}
	}

	return false, nil
}

// executeAction runs the executes the given palybook action.
// It should received an already templated action spec.
func executeAction(ctx context.Context, playbookID, runID uuid.UUID, runAction models.PlaybookRunAction, actionSpec v1.PlaybookAction) (*executeActionResult, error) {
	ctx, span := ctx.StartSpan("executeAction")
	defer span.End()

	logger.WithValues("runID", runID).Infof("Executing action: %s", actionSpec.Name)

	if timeout, _ := actionSpec.TimeoutDuration(); timeout > 0 {
		var cancel gocontext.CancelFunc
		ctx, cancel = ctx.WithTimeout(timeout)
		defer cancel()
	}

	if actionSpec.Filter != "" {
		if skip, err := filterAction(ctx, runID, actionSpec.Filter); err != nil {
			return nil, err
		} else if skip {
			return &executeActionResult{skipped: true}, nil
		}
	}

	if actionSpec.AzureDevopsPipeline != nil {
		var e actions.AzureDevopsPipeline
		res, err := e.Run(ctx, *actionSpec.AzureDevopsPipeline)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.Github != nil {
		var e actions.Github
		res, err := e.Run(ctx, *actionSpec.Github)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.Exec != nil {
		var e actions.ExecAction
		res, err := e.Run(ctx, *actionSpec.Exec)
		if err != nil {
			return nil, err
		}

		if err := saveArtifacts(ctx, runAction.ID, res.Artifacts); err != nil {
			return nil, fmt.Errorf("error saving artifacts: %v", err)
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.HTTP != nil {
		var e actions.HTTP
		res, err := e.Run(ctx, *actionSpec.HTTP)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.SQL != nil {
		var e actions.SQL
		res, err := e.Run(ctx, *actionSpec.SQL)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.Pod != nil {
		e := actions.Pod{
			PlaybookRunID: runID,
			PlaybookID:    playbookID,
		}

		timeout, _ := actionSpec.TimeoutDuration()
		res, err := e.Run(ctx, *actionSpec.Pod, timeout)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.GitOps != nil {
		var e actions.GitOps
		res, err := e.Run(ctx, *actionSpec.GitOps)
		if err != nil {
			return nil, err
		}

		jsonData, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: jsonData}, nil
	}

	if actionSpec.Notification != nil {
		var e actions.Notification
		err := e.Run(ctx, *actionSpec.Notification)
		if err != nil {
			return nil, err
		}

		return &executeActionResult{data: []byte("{}")}, nil
	}

	return &executeActionResult{}, nil
}
