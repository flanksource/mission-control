package jobs

import (
	"fmt"
	"io"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/playbook"
)

// PullPlaybookActions periodically pulls playbook actions to run
// from the upstream
var PullPlaybookActions = &job.Job{
	Name:       "PullPlaybookActions",
	Schedule:   "@every 10s",
	JobHistory: true,
	RunNow:     true,
	Singleton:  false,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypePlaybook
		ctx.History.ResourceID = api.UpstreamConf.Host
		if pulled, err := pullPlaybookAction(ctx.Context); err != nil {
			return err
		} else if pulled {
			ctx.History.SuccessCount = 1
		}

		return nil
	},
}

// PullPlaybookActions pushes actions, that have been fully run, to the upstream
var PushPlaybookActions = &job.Job{
	Name:       "PushPlaybookActions",
	Schedule:   "@every 10s",
	JobHistory: true,
	RunNow:     true,
	Singleton:  false,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypePlaybook
		ctx.History.ResourceID = api.UpstreamConf.Host
		if count, err := pushPlaybookActions(ctx.Context, 200); err != nil {
			return err
		} else {
			ctx.History.SuccessCount += count
		}

		return nil
	},
}

// syncPlaybookActions pushes unpushed playbook actions to the upstream
func pushPlaybookActions(ctx context.Context, batchSize int) (int, error) {
	client := upstream.NewUpstreamClient(api.UpstreamConf)
	count := 0
	for {
		var actions []models.PlaybookRunAction
		if err := ctx.DB().Select("id, status, result, error, end_time").
			Where("is_pushed IS FALSE").
			Where("status IN ?", models.PlaybookActionFinalStates).
			Limit(batchSize).
			Find(&actions).Error; err != nil {
			return 0, fmt.Errorf("failed to fetch playbook_run_actions: %w", err)
		}

		if len(actions) == 0 {
			return count, nil
		}

		ctx.Tracef("pushing %d playbook actions to upstream", len(actions))
		if err := client.Push(ctx, &upstream.PushData{PlaybookActions: actions, AgentName: api.UpstreamConf.AgentName}); err != nil {
			return 0, fmt.Errorf("failed to push playbook actions to upstream: %w", err)
		}

		ids := make([]uuid.UUID, len(actions))
		for i := range actions {
			ids[i] = actions[i].ID
		}
		if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id IN ?", ids).Update("is_pushed", true).Error; err != nil {
			return 0, fmt.Errorf("failed to update is_pushed on playbook actions: %w", err)
		}

		count += len(actions)
	}
}

func pullPlaybookAction(ctx context.Context) (bool, error) {
	client := upstream.NewUpstreamClient(api.UpstreamConf)

	req := client.R(ctx)
	resp, err := req.Get("playbook-action")
	if err != nil {
		return false, fmt.Errorf("error pushing to upstream: %w", err)
	}
	defer resp.Body.Close()

	if !resp.IsOK() {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return false, fmt.Errorf("upstream server returned error status[%d]: %s", resp.StatusCode, string(respBody))
	}

	var response playbook.ActionForAgent
	if err := resp.Into(&response); err != nil {
		return false, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid response: %v", err)
	}

	if response.Action == nil {
		return false, nil
	}

	if err := ctx.DB().Omit("playbook_run_id").Save(response.Action).Error; err != nil {
		return false, fmt.Errorf("failed to save playbook action: %w", err)
	}

	columnUpdates := map[string]any{
		"end_time": gorm.Expr("CLOCK_TIMESTAMP()"),
	}

	result, err := playbook.ExecuteAction(ctx, lo.FromPtr(response.Run), lo.FromPtr(response.Action), lo.FromPtr(response.ActionSpec), lo.FromPtr(response.TemplateEnv))
	if err != nil {
		logger.Errorf("failed to execute action: %v", err)
		columnUpdates["status"] = models.PlaybookActionStatusFailed
		columnUpdates["error"] = err.Error()
	} else if result.Skipped {
		columnUpdates["status"] = models.PlaybookActionStatusSkipped
	} else {
		columnUpdates["status"] = models.PlaybookActionStatusCompleted
		columnUpdates["result"] = result.Data
	}
	if err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("id = ?", response.Action.ID).UpdateColumns(&columnUpdates).Error; err != nil {
		return false, fmt.Errorf("failed to update playbook action result: %w", err)
	}

	return true, nil
}
