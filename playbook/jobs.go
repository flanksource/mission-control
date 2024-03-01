package playbook

import (
	"encoding/json"
	"fmt"
	"io"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/google/uuid"
)

// PushPlaybookActions pushes unpushed playbook actions to the upstream
func PushPlaybookActions(ctx context.Context, upstreamConfig upstream.UpstreamConfig, batchSize int) (int, error) {
	client := upstream.NewUpstreamClient(upstreamConfig)
	count := 0
	for {
		var actions []models.PlaybookRunAction
		if err := ctx.DB().Select("id, status, result, error, start_time, end_time").
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
		if err := client.Push(ctx, &upstream.PushData{PlaybookActions: actions}); err != nil {
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

// PullPlaybookAction pulls any runnable Playbook Action from the upstream
// and simply saves it.
func PullPlaybookAction(ctx context.Context, upstreamConfig upstream.UpstreamConfig) (bool, error) {
	client := upstream.NewUpstreamClient(upstreamConfig)

	req := client.R(ctx).QueryParam(upstream.AgentNameQueryParam, upstreamConfig.AgentName)
	resp, err := req.Get("playbook-action")
	if err != nil {
		return false, fmt.Errorf("error pushing to upstream: %w", err)
	}
	defer resp.Body.Close()

	if !resp.IsOK() {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return false, fmt.Errorf("upstream server returned error status[%d]: %s", resp.StatusCode, string(respBody))
	}

	var response ActionForAgent
	if err := resp.Into(&response); err != nil {
		return false, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid response: %v", err)
	}

	if response.Action.ID == uuid.Nil {
		return false, nil
	}

	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())

	// Don't save playbook_run_id to avoid foreign key constraint
	if err := ctx.DB().Omit("playbook_run_id").Save(&response.Action).Error; err != nil {
		return false, fmt.Errorf("failed to save playbook action: %w", err)
	}

	actionData := models.PlaybookActionAgentData{
		ActionID:   response.Action.ID,
		PlaybookID: response.Run.PlaybookID,
		RunID:      response.Run.ID,
	}

	actionData.Spec, err = json.Marshal(response.ActionSpec)
	if err != nil {
		return false, fmt.Errorf("failed to marshal action spec: %w", err)
	}

	actionData.Env, err = json.Marshal(response.TemplateEnv)
	if err != nil {
		return false, fmt.Errorf("failed to marshal action template env: %w", err)
	}

	if err := ctx.DB().Create(&actionData).Error; err != nil {
		return false, fmt.Errorf("failed to save playbook action data for the agent: %w", err)
	}

	return true, tx.Commit().Error
}
