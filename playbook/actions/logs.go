package actions

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/gomplate/v3"
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
			ContentType: "application/log+json", // so UI can distinguish between json and logs in JSON
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
		searcher := loki.NewSearcher(action.Loki.Loki, action.Loki.Mapping)
		response, err := searcher.Fetch(ctx, action.Loki.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from loki")
		}

		response.Logs = postProcessLogs(ctx, response.Logs, action.Loki.LogsPostProcess)
		return (*logsResult)(response), nil
	}

	if action.OpenSearch != nil {
		searcher, err := opensearch.NewSearcher(ctx, action.OpenSearch.Backend, action.OpenSearch.Mapping)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to create opensearch searcher")
		}

		response, err := searcher.Search(ctx, &action.OpenSearch.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from opensearch")
		}

		response.Logs = postProcessLogs(ctx, response.Logs, action.OpenSearch.LogsPostProcess)
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

		searcher := cloudwatch.NewSearcher(client, cw.Mapping)
		response, err := searcher.Search(ctx, cw.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from cloudwatch")
		}

		response.Logs = postProcessLogs(ctx, response.Logs, action.CloudWatch.LogsPostProcess)
		return (*logsResult)(response), nil
	}

	return nil, nil
}

func postProcessLogs(ctx context.Context, logLines []logs.LogLine, postProcess v1.LogsPostProcess) []logs.LogLine {
	if postProcess.Empty() {
		return logLines
	}

	filteredLogs := dedupLogs(logLines, postProcess.Dedupe)
	matchedLogs := matchLogs(ctx, filteredLogs, string(postProcess.Match))
	return matchedLogs
}

func dedupLogs(logLines []logs.LogLine, dedupFields []string) []logs.LogLine {
	if len(dedupFields) == 0 {
		return logLines
	}

	return logLines
}

func matchLogs(ctx context.Context, logLines []logs.LogLine, matchExpr string) []logs.LogLine {
	if matchExpr == "" {
		return logLines
	}

	var matchedLogs []logs.LogLine
	expr := gomplate.Template{Expression: matchExpr}
	for _, logLine := range logLines {
		ok, err := gomplate.RunTemplateBool(logLine.TemplateContext(), expr)
		if err != nil {
			ctx.Logger.V(4).Infof("failed to evaluate match expression '%s': %v", matchExpr, err)
			continue
		}

		if ok {
			matchedLogs = append(matchedLogs, logLine)
		}
	}

	return matchedLogs
}
