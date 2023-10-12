package playbook

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"gorm.io/gorm"
)

func ExecuteRun(ctx api.Context, run models.PlaybookRun) {
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

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumn("status", models.PlaybookRunStatusRunning).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
		return
	}

	columnUpdates := map[string]any{
		"end_time": "NOW()",
	}

	if runResponse, err := executeRun(ctx, run, runOptions); err != nil {
		logger.Errorf("failed to execute playbook run: %v", err)
		columnUpdates["status"] = models.PlaybookRunStatusFailed
	} else if runResponse.Sleep > 0 {
		columnUpdates["start_time"] = gorm.Expr(fmt.Sprintf("NOW() + INTERVAL '%d SECONDS'", int(runResponse.Sleep.Seconds())))
		columnUpdates["status"] = models.PlaybookRunStatusSleeping
	} else {
		columnUpdates["status"] = models.PlaybookRunStatusCompleted
	}

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
	}
}

type runExecOptions struct {
	StartFrom *models.PlaybookRunAction
}

type runExecResponse struct {
	// Sleep, when set, indicates that the run execution should pause and continue
	// after the specified duration.
	Sleep time.Duration
}

func executeRun(ctx api.Context, run models.PlaybookRun, opt runExecOptions) (*runExecResponse, error) {
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
		WithValues("component", run.ComponentID).
		Infof("Executing playbook run: %s", run.ID)

	templateEnv := actions.TemplateEnv{
		Params: run.Parameters,
	}
	if run.ComponentID != nil {
		if err := ctx.DB().Where("id = ?", run.ComponentID).First(&templateEnv.Component).Error; err != nil {
			return nil, err
		}
	} else if run.ConfigID != nil {
		if err := ctx.DB().Where("id = ?", run.ConfigID).First(&templateEnv.Config).Error; err != nil {
			return nil, err
		}
	} else if run.CheckID != nil {
		if err := ctx.DB().Where("id = ?", run.CheckID).First(&templateEnv.Check).Error; err != nil {
			return nil, err
		}
	}

	var continueFromAction string
	if opt.StartFrom != nil {
		continueFromAction = opt.StartFrom.Name
	}

	for _, action := range playbook.Spec.Actions {
		if continueFromAction != "" && action.Name != continueFromAction {
			continue
		}

		logger.WithValues("runID", run.ID).Infof("Executing action: %s", action.Name)

		runAction := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          action.Name,
			Status:        models.PlaybookRunStatusRunning,
		}
		if opt.StartFrom != nil {
			runAction = *opt.StartFrom
			runAction.Status = models.PlaybookRunStatusRunning
		}

		if err := ctx.DB().Save(&runAction).Error; err != nil {
			logger.Errorf("failed to create playbook run action: %v", err)
			return nil, err
		}

		columnUpdates := map[string]any{
			"end_time": "NOW()",
		}

		if duration, err := action.DelayDuration(); err != nil {
			return nil, err
		} else if duration > 0 && action.Name != continueFromAction {
			logger.Debugf("Pausing run execution. Sleeping for %v", duration)

			if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", runAction.ID).UpdateColumn("status", models.PlaybookRunStatusSleeping).Error; err != nil {
				logger.Errorf("failed to update playbook run action status: %v", err)
			}

			return &runExecResponse{
				Sleep: duration,
			}, nil
		} else if action.Name == continueFromAction {
			continueFromAction = ""
		}

		result, err := executeAction(ctx, run, action, templateEnv)
		if err != nil {
			logger.Errorf("failed to execute action: %v", err)
			columnUpdates["status"] = models.PlaybookRunStatusFailed
			columnUpdates["error"] = err.Error()
		} else {
			columnUpdates["status"] = models.PlaybookRunStatusCompleted
			columnUpdates["result"] = result
		}

		if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", runAction.ID).UpdateColumns(&columnUpdates).Error; err != nil {
			logger.Errorf("failed to update playbook run action status: %v", err)
		}

		// Even if a single action fails, we stop the execution and mark the Run as failed
		if err != nil {
			return nil, fmt.Errorf("action %q failed: %w", action.Name, err)
		}
	}

	return &runExecResponse{}, nil
}

func executeAction(ctx api.Context, run models.PlaybookRun, action v1.PlaybookAction, env actions.TemplateEnv) ([]byte, error) {
	if action.Exec != nil {
		var e actions.ExecAction
		res, err := e.Run(ctx, *action.Exec, env)
		if err != nil {
			return nil, err
		}

		return json.Marshal(res)
	}

	if action.HTTP != nil {
		var e actions.HTTP
		res, err := e.Run(ctx, *action.HTTP, env)
		if err != nil {
			return nil, err
		}

		return json.Marshal(res)
	}

	if action.SQL != nil {
		var e actions.SQL
		res, err := e.Run(ctx, *action.SQL, env)
		if err != nil {
			return nil, err
		}

		return json.Marshal(res)
	}

	if action.Pod != nil {
		e := actions.Pod{
			PlaybookRun: run,
		}
		res, err := e.Run(ctx, *action.Pod, env)
		if err != nil {
			return nil, err
		}

		return json.Marshal(res)
	}

	return nil, nil
}
