package playbook

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func ProcessRunQueue(ctx *api.Context) error {
	runs, err := db.GetSchedulePlaybookRuns(ctx)
	if err != nil {
		return fmt.Errorf("failed to get playbook runs: %w", err)
	}

	if len(runs) == 0 {
		return nil
	}

	logger.Infof("Processing %d playbook runs", len(runs))

	for _, r := range runs {
		logger.Infof("running %v", r)
		go func(run models.PlaybookRun) {
			if err := executeRun(ctx, run); err != nil {
				logger.Errorf("failed to execute playbook run: %v", err)
			}
		}(r)
	}

	return nil
}

func executeRun(ctx *api.Context, run models.PlaybookRun) error {
	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumn("status", models.PlaybookRunStatusRunning).Error; err != nil {
		return err
	}

	// The actual job here
	time.Sleep(time.Second * 3)

	if err := ctx.DB().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumn("status", models.PlaybookRunStatusCompleted).Error; err != nil {
		return err
	}

	return nil
}
