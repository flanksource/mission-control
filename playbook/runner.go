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

	if result, err := executeRun(ctx, run); err != nil {
		logger.Errorf("failed to execute playbook run: %v", err)
		columnUpdates["status"] = models.PlaybookRunStatusFailed
		columnUpdates["error"] = err.Error()
	} else {
		columnUpdates["status"] = models.PlaybookRunStatusCompleted

		resultJson, _ := json.Marshal(result)
		columnUpdates["result"] = resultJson
	}

	columnUpdates["duration"] = time.Since(start).Milliseconds()
	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
	}
}

func executeRun(ctx *api.Context, run models.PlaybookRun) ([]map[string]any, error) {
	var playbookModel models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbookModel).Error; err != nil {
		return nil, err
	}

	playbook, err := v1.PlaybookFromModel(playbookModel)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(playbook.Spec.Actions))
	for _, action := range playbook.Spec.Actions {
		if action.Exec != nil {
			e := ExecAction{}
			res, err := e.Run(ctx, *action.Exec)
			if err != nil {
				return nil, err
			}

			result = append(result, map[string]any{
				"exec": res.Stdout,
			})
		}
	}

	return result, nil
}
