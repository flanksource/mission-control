package playbook

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

var (
	lastResultCache = cache.New(time.Minute*15, time.Minute*30)
)

// validateAndSavePlaybookRun creates and saves a run from a run request after validating the run parameters.
func validateAndSavePlaybookRun(ctx context.Context, playbook *models.Playbook, req RunParams) (*models.PlaybookRun, error) {
	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return nil, err
	}

	if err := req.validateParams(spec.Parameters); err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid parameters: %v", err)
	}

	run := models.PlaybookRun{
		PlaybookID: playbook.ID,
		Status:     models.PlaybookRunStatusPending,
		Parameters: req.Params,
	}

	if ctx.User() != nil {
		run.CreatedBy = &ctx.User().ID
	}

	if spec.Approval == nil || spec.Approval.Approvers.Empty() {
		run.Status = models.PlaybookRunStatusScheduled
	}

	if req.ComponentID != uuid.Nil {
		run.ComponentID = &req.ComponentID
	}

	if req.ConfigID != uuid.Nil {
		run.ConfigID = &req.ConfigID
	}

	if req.CheckID != uuid.Nil {
		run.CheckID = &req.CheckID
	}

	if req.Request != nil {
		whr, err := collections.StructToJSON(req.Request)
		if err != nil {
			return nil, fmt.Errorf("error marshalling webhook request to json: %w", err)
		}
		var whrMap map[string]any
		if err := json.Unmarshal([]byte(whr), &whrMap); err != nil {
			return nil, fmt.Errorf("error unmarshalling webhook request from json: %w", err)
		}
		run.Request = whrMap
	}

	templateEnv, err := prepareTemplateEnv(ctx, *playbook, run)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare template env: %w", err)
	}

	if err := req.setDefaults(ctx, spec, templateEnv); err != nil {
		return nil, fmt.Errorf("failed to set defaults: %v", err)
	}
	run.Parameters = types.JSONStringMap{}

	for k, v := range req.Params {
		run.Parameters[k] = fmt.Sprintf("%v", v)
	}

	// Check playbook filters
	if err := checkPlaybookFilter(ctx, spec, templateEnv); err != nil {
		return nil, err
	}

	if err := savePlaybookRun(ctx, playbook, &run); err != nil {
		return nil, fmt.Errorf("failed to create playbook run: %v", err)
	}

	return &run, nil
}

// savePlaybookRun saves the run and attempts register an approval from the caller.
func savePlaybookRun(ctx context.Context, playbook *models.Playbook, run *models.PlaybookRun) error {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())

	if err := ctx.DB().Create(run).Error; err != nil {
		return err
	}

	// Attempt to auto approve run
	if err := approveRun(ctx, playbook, run.ID); err != nil {
		switch dutyAPI.ErrorCode(err) {
		case dutyAPI.EFORBIDDEN, dutyAPI.EINVALID:
			// ignore these errors
		default:
			return fmt.Errorf("error while attempting to auto approve run: %w", err)
		}
	}

	return tx.Commit().Error
}

func ListPlaybooksForConfig(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", id).Find(&config).Error; err != nil {
		return nil, err
	} else if config.ID == uuid.Nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", id)
	}

	return db.FindPlaybooksForConfig(ctx, config)
}

func ListPlaybooksForComponent(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var component models.Component
	if err := ctx.DB().Where("id = ?", id).Find(&component).Error; err != nil {
		return nil, err
	} else if component.ID == uuid.Nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "component(id=%s) not found", id)
	}

	return db.FindPlaybooksForComponent(ctx, component)
}

func ListPlaybooksForCheck(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var check models.Check
	if err := ctx.DB().Where("id = ?", id).Find(&check).Error; err != nil {
		return nil, err
	} else if check.ID == uuid.Nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "check(id=%s) not found", id)
	}

	return db.FindPlaybooksForCheck(ctx, check)
}

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

// evaluateRunStatus determines the best fitting run status based on the status of the actions.
func evaluateRunStatus(statuses []models.PlaybookActionStatus) models.PlaybookRunStatus {
	for _, status := range statuses {
		if status == models.PlaybookActionStatusFailed {
			return models.PlaybookRunStatusFailed
		}
	}

	return models.PlaybookRunStatusCompleted
}
