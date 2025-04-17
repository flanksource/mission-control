package actions

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/logs/cloudwatch"
	"github.com/flanksource/incident-commander/logs/loki"
)

type logsAction struct {
}

type logsResult struct {
	Metadata any `json:"metadata,omitempty"`

	// Saved to artifacts
	logs string
}

func (t *logsResult) GetArtifacts() []artifacts.Artifact {
	return []artifacts.Artifact{
		{
			ContentType: "application/json",
			Content:     io.NopCloser(strings.NewReader(string(t.logs))),
			Path:        "logs.json",
		},
	}
}

func NewLogsAction() *logsAction {
	return &logsAction{}
}

func (l *logsAction) Run(ctx context.Context, action *v1.LogsAction) (*logsResult, error) {
	if action.Loki != nil {
		response, err := loki.Fetch(ctx, action.Loki.BaseURL, action.Loki.Authentication, action.Loki.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from loki")
		}

		return &logsResult{
			Metadata: response.Data.Stats,
			logs:     string(response.Data.Result),
		}, nil
	}

	if action.CloudWatch != nil {
		cw := action.CloudWatch

		if err := cw.AWSConnection.Populate(ctx); err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to populate aws config for cloudwatch")
		}

		awsConfig, err := cw.AWSConnection.Client(ctx)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to get aws config for cloudwatch")
		}

		client := cloudwatchlogs.NewFromConfig(awsConfig)

		searcher := cloudwatch.NewSearcher(client)
		response, err := searcher.Search(ctx, cw.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from cloudwatch")
		}

		events, err := json.Marshal(response.Events)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to json marshal cloudwatch logs")
		}

		return &logsResult{
			logs: string(events),
		}, nil
	}

	return nil, nil
}
