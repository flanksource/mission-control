package playbook

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

func ProcessRunQueue(ctx *api.Context) error {
	runs, err := db.GetScheduledPlaybookRuns(ctx)
	if err != nil {
		return fmt.Errorf("failed to get playbook runs: %w", err)
	}

	if len(runs) == 0 {
		return nil
	}

	logger.Infof("Starting to execute %d playbook runs", len(runs))

	for _, r := range runs {
		go ExecuteRun(ctx, r)
	}

	return nil
}

func ExecuteRun(ctx *api.Context, run models.PlaybookRun) {
	logger.Infof("Executing playbook run: %s", run.ID)

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumn("status", models.PlaybookRunStatusRunning).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
		return
	}

	start := time.Now()
	columnUpdates := map[string]any{
		"end_time": "NOW()",
	}

	if err := executeRun(ctx, run); err != nil {
		logger.Errorf("failed to execute playbook run: %v", err)
		columnUpdates["status"] = models.PlaybookRunStatusFailed
	} else {
		columnUpdates["status"] = models.PlaybookRunStatusCompleted
	}

	columnUpdates["duration"] = time.Since(start).Milliseconds()
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

	for _, action := range playbook.Spec.Actions {
		logger.Infof("Executing playbook run: %s", run.ID)

		runAction := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          action.Name,
			Status:        models.PlaybookRunStatusRunning,
		}

		if err := ctx.DB().Create(&runAction).Error; err != nil {
			logger.Errorf("failed to create playbook run action: %v", err)
			return err
		}

		start := time.Now()
		columnUpdates := map[string]any{
			"end_time": "NOW()",
		}

		result, err := executeAction(ctx, run, action)
		if err != nil {
			logger.Errorf("failed to execute action: %v", err)
			columnUpdates["status"] = models.PlaybookRunStatusFailed
			columnUpdates["error"] = err.Error()
		} else {
			columnUpdates["status"] = models.PlaybookRunStatusCompleted
			columnUpdates["result"] = result
		}

		columnUpdates["duration"] = time.Since(start).Milliseconds()
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

func executeAction(ctx *api.Context, run models.PlaybookRun, action v1.PlaybookAction) ([]byte, error) {
	if action.Exec != nil {
		e := ExecAction{}
		res, err := e.Run(ctx, *action.Exec)
		if err != nil {
			return nil, err
		}

		return json.Marshal(res.Stdout)
	}

	return nil, nil
}
