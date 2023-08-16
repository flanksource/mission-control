package playbook

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
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
		columnUpdates["result"] = err.Error()
	} else {
		columnUpdates["status"] = models.PlaybookRunStatusCompleted
	}

	columnUpdates["duration"] = time.Since(start).Milliseconds()
	if err := ctx.DB().Debug().Model(&models.PlaybookRun{}).Where("id = ?", run.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		logger.Errorf("failed to update playbook run status: %v", err)
	}
}

func executeRun(ctx *api.Context, run models.PlaybookRun) error {
	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbook).Error; err != nil {
		return err
	}

	// The actual job here
	time.Sleep(time.Second * time.Duration(rand.Intn(4)))

	if rand.Intn(4) != 0 { // 75% chance of failing
		return nil
	}

	return errors.New("dummy error")
}
