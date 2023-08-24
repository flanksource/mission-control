package playbook

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
)

// ActionParam defines the config and component passed to a playbook run action.
type ActionParam struct {
	Config    *models.ConfigItem `json:"config,omitempty"`
	Component *models.Component  `json:"component,omitempty"`
	Params    map[string]string  `json:"params,omitempty"`
}

func (t *ActionParam) AsMap() map[string]any {
	return map[string]any{
		"config":    t.Config,
		"component": t.Component,
		"params":    t.Params,
	}
}

func ExecuteRun(ctx *api.Context, run models.PlaybookRun) {
	logger.Infof("Executing playbook run: %s", run.ID)

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumn("status", models.PlaybookRunStatusRunning).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
		return
	}

	columnUpdates := map[string]any{
		"end_time": "NOW()",
	}

	if err := executeRun(ctx, run); err != nil {
		logger.Errorf("failed to execute playbook run: %v", err)
		columnUpdates["status"] = models.PlaybookRunStatusFailed
	} else {
		columnUpdates["status"] = models.PlaybookRunStatusCompleted
	}

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
	}
}

func executeRun(ctx *api.Context, run models.PlaybookRun) error {
	var playbookModel models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbookModel).Error; err != nil {
		return err
	}

	playbook, err := v1.PlaybookFromModel(playbookModel)
	if err != nil {
		return err
	}

	actionParam := ActionParam{
		Params: run.Parameters,
	}
	if run.ComponentID != nil {
		if err := ctx.DB().Where("id = ?", run.ComponentID).First(&actionParam.Component).Error; err != nil {
			return err
		}
	} else if run.ConfigID != nil {
		if err := ctx.DB().Where("id = ?", run.ConfigID).First(&actionParam.Config).Error; err != nil {
			return err
		}
	}

	for _, action := range playbook.Spec.Actions {
		logger.WithValues("runID", run.ID).Infof("Executing action: %s", action.Name)

		runAction := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          action.Name,
			Status:        models.PlaybookRunStatusRunning,
		}

		if err := ctx.DB().Create(&runAction).Error; err != nil {
			logger.Errorf("failed to create playbook run action: %v", err)
			return err
		}

		columnUpdates := map[string]any{
			"end_time": "NOW()",
		}

		result, err := executeAction(ctx, run, action, actionParam)
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
			return fmt.Errorf("action %s failed: %w", action.Name, err)
		}
	}

	return nil
}

func executeAction(ctx *api.Context, run models.PlaybookRun, action v1.PlaybookAction, actionParam ActionParam) ([]byte, error) {
	if action.Exec != nil {
		script, err := gomplate.RunTemplate(actionParam.AsMap(), gomplate.Template{Template: action.Exec.Script})
		if err != nil {
			return nil, err
		}
		action.Exec.Script = script

		e := actions.ExecAction{}
		res, err := e.Run(ctx, *action.Exec)
		if err != nil {
			return nil, err
		}

		return json.Marshal(res)
	}

	return nil, nil
}
