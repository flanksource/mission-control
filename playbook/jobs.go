package playbook

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

var pushPullLagBuckets = []float64{10, 100, 200, 500, 1000, 3000, 5000, 10_000, 20_000, 30_000, 60_000, 100_000}

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

		for _, action := range actions {
			if action.EndTime == nil {
				ctx.Warnf("attempted to push action with null end time. action=%s, run=%s", action.ID, action.PlaybookRunID)
			} else {
				lag := time.Since(*action.EndTime)
				ctx.Histogram("playbook_action_push_lag", pushPullLagBuckets,
					"upstream", upstreamConfig.Host,
					"agent", upstreamConfig.AgentName).
					Record(time.Duration(lag.Milliseconds()))
			}
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
func PullPlaybookAction(ctx job.JobRuntime, upstreamConfig upstream.UpstreamConfig) error {
	client := upstream.NewUpstreamClient(upstreamConfig)

	oops := ctx.Oops()

	req := client.R(ctx).QueryParam(upstream.AgentNameQueryParam, upstreamConfig.AgentName)
	resp, err := req.Get("playbook-action")
	if err != nil {
		return oops.Wrapf(err, "error pushing to upstream")
	}
	defer resp.Body.Close()

	if !resp.IsOK() {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return oops.Hint(string(respBody)).Errorf("upstream returned code=%d", resp.StatusCode)
	}

	var response runner.ActionForAgent
	if err := resp.Into(&response); err != nil {
		return oops.Code(dutyAPI.EINVALID).Wrapf(err, "invalid response")
	}

	if response.Action.ID == uuid.Nil {
		return nil
	}

	ctx.Histogram("playbook_action_pull_lag", pushPullLagBuckets,
		"upstream", upstreamConfig.Host,
		"agent", upstreamConfig.AgentName).
		Record(time.Duration(time.Since(response.Action.ScheduledTime).Milliseconds()))

	err = ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		// Don't save playbook_run_id to avoid foreign key constraint
		if err := ctx.DB().Omit("playbook_run_id").Save(&response.Action).Error; err != nil {
			return oops.Wrap(err)
		}

		actionData := models.PlaybookActionAgentData{
			ActionID:   response.Action.ID,
			PlaybookID: response.Run.PlaybookID,
			RunID:      response.Run.ID,
		}

		actionData.Spec, err = json.Marshal(response.ActionSpec)
		if err != nil {
			return ctx.Oops().Wrap(err)
		}

		actionData.Env, err = json.Marshal(response.TemplateEnv)
		if err != nil {
			return ctx.Oops().Wrap(err)
		}
		return ctx.Oops("db").Wrap(ctx.DB().Create(&actionData).Error)
	}, "save_action")

	if err == nil {
		ctx.History.IncrSuccess()
	} else {
		ctx.History.AddError(err.Error())
	}

	return err
}
