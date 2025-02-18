package runner

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
)

var (
	lastResultCache = cache.New(time.Minute*15, time.Minute*30)
)

func GetLastAction(ctx context.Context, runID, callerActionID string) (map[string]any, error) {
	if callerActionID == "" || callerActionID == uuid.Nil.String() {
		return nil, nil
	}

	if cached, ok := lastResultCache.Get("last-action" + runID + callerActionID); ok {
		ctx.Logger.V(4).Infof("get last action run=%s: %s ==> using cache", runID, callerActionID)
		return cached.(map[string]any), nil
	}

	var action models.PlaybookRunAction
	query := ctx.DB().
		Where("id != ?", callerActionID).
		Where("playbook_run_id = ?", runID).
		Order("start_time desc")
	if err := query.First(&action).Error; err != nil {
		ctx.Logger.V(4).Infof("get last action run=%s: %s ==> not found", runID, callerActionID)
		return nil, err
	}

	ctx.Logger.V(5).Infof("getLastAction ==> %s", logger.Pretty(action))
	output := action.AsMap()
	lastResultCache.SetDefault("last-action"+runID+callerActionID, output)
	return output, nil
}

func GetActionByName(ctx context.Context, runID, actionName string) (map[string]any, error) {
	if cached, ok := lastResultCache.Get("action-by-name" + runID + actionName); ok {
		ctx.Logger.V(4).Infof("getActionByName run=%s: %s ==> using cache", runID, actionName)

		return cached.(map[string]any), nil
	}

	var action models.PlaybookRunAction
	query := ctx.DB().Where("name = ?", actionName).Where("playbook_run_id = ?", runID)

	// there could be multiple actions with the same name due to retries.
	// we fetch the latest one
	query.Order("start_time ASC").Limit(1)

	if err := query.First(&action).Error; err != nil {
		ctx.Logger.V(4).Infof("getActionByName run=%s: %s ==> not found", runID, actionName)
		return nil, err
	}

	output := action.AsMap()
	ctx.Logger.V(5).Infof("getActionByName ==> %s", logger.Pretty(action))
	lastResultCache.SetDefault("action-by-name"+runID+actionName, output)
	return output, nil
}
