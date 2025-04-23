package actions

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/logs/cloudwatch"
	"github.com/flanksource/incident-commander/logs/loki"
	"github.com/flanksource/incident-commander/logs/opensearch"
)

type logsAction struct {
}

type logsResult struct {
	Metadata map[string]any `json:"metadata,omitempty"`
	Logs     []logs.LogLine `json:"-"`
}

func (t *logsResult) GetArtifacts() []artifacts.Artifact {
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(t.Logs); err != nil {
		logger.Errorf("failed to json marshal logs: %v", err)
		return nil
	}

	return []artifacts.Artifact{
		{
			ContentType: "application/json",
			Content:     io.NopCloser(&b),
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

		return (*logsResult)(response), nil
	}

	if action.OpenSearch != nil {
		searcher, err := opensearch.NewSearcher(ctx, action.OpenSearch.Backend)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to create opensearch searcher")
		}

		response, err := searcher.Search(ctx, &action.OpenSearch.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from opensearch")
		}

		return (*logsResult)(response), nil
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

		return (*logsResult)(response), nil
	}

	return nil, nil
}
