package runner

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/patrickmn/go-cache"
)

var (
	lastResultCache = cache.New(time.Minute*15, time.Minute*30)
)

func GetLastAction(ctx context.Context, runID, callerActionID string) (map[string]any, error) {
	if cached, ok := lastResultCache.Get("last-action" + runID + callerActionID); ok {
		return cached.(map[string]any), nil
	}

	var action models.PlaybookRunAction
	query := ctx.DB().
		Where("id != ?", callerActionID).
		Where("playbook_run_id = ?", runID).
		Order("start_time desc")
	if err := query.First(&action).Error; err != nil {
		return nil, err
	}

	output := action.AsMap()
	lastResultCache.SetDefault("last-action"+runID+callerActionID, output)
	return output, nil
}

func GetActionByName(ctx context.Context, runID, actionName string) (map[string]any, error) {
	if cached, ok := lastResultCache.Get("action-by-name" + runID + actionName); ok {
		return cached.(map[string]any), nil
	}

	var action models.PlaybookRunAction
	query := ctx.DB().Where("name = ?", actionName).Where("playbook_run_id = ?", runID)
	if err := query.First(&action).Error; err != nil {
		return nil, err
	}

	output := action.AsMap()
	lastResultCache.SetDefault("action-by-name"+runID+actionName, output)
	return output, nil
}
