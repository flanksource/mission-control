package actions

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
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
	Metadata map[string]any  `json:"metadata,omitempty"`
	Logs     []*logs.LogLine `json:"-"`
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

func postProcessLogs(ctx context.Context, logLines []*logs.LogLine, postProcess v1.LogsPostProcess) []*logs.LogLine {
	if postProcess.Empty() {
		return logLines
	}

	matchedLogs := matchLogs(ctx, logLines, postProcess.Match)
	filteredLogs := dedupLogs(matchedLogs, postProcess.Dedupe)
	return filteredLogs
}

func dedupLogs(logLines []*logs.LogLine, dedupFields []string) []*logs.LogLine {
	if len(dedupFields) == 0 {
		return logLines
	}

	dedupedLogs := make([]*logs.LogLine, 0, len(logLines))

	seen := make(map[string]*logs.LogLine)
outer:
	for _, logLine := range logLines {
		for _, field := range dedupFields {
			fieldValue := logLine.GetDedupField(field)
			if foundLogLine, ok := seen[fieldValue]; ok {
				foundLogLine.Count++

				if logLine.FirstObserved.Before(foundLogLine.FirstObserved) {
					foundLogLine.FirstObserved = logLine.FirstObserved
				} else {
					foundLogLine.Message = logLine.Message
				}

				if l := maxTime(&logLine.FirstObserved, logLine.LastObserved, &foundLogLine.FirstObserved, foundLogLine.LastObserved); !l.IsZero() {
					foundLogLine.LastObserved = &l
				}

				continue outer
			}

			seen[fieldValue] = logLine
		}

		dedupedLogs = append(dedupedLogs, logLine)
	}

	return dedupedLogs
}

func matchLogs(ctx context.Context, logLines []*logs.LogLine, matchExprs []types.MatchExpression) []*logs.LogLine {
	if len(matchExprs) == 0 {
		return logLines
	}

	var matchedLogs []*logs.LogLine

outer:
	for _, logLine := range logLines {
		env := logLine.TemplateContext()

		for _, matchExpr := range matchExprs {
			expr := gomplate.Template{Expression: string(matchExpr)}
			ok, err := gomplate.RunTemplateBool(env, expr)
			if err != nil {
				ctx.Logger.V(4).Infof("failed to evaluate match expression '%s': %v", matchExprs, err)
				continue
			}

			if ok {
				matchedLogs = append(matchedLogs, logLine)
				continue outer
			}
		}
	}

	return matchedLogs
}

func maxTime(timestamps ...*time.Time) time.Time {
	max := time.Time{}
	for _, t := range timestamps {
		if t == nil {
			continue
		}

		if t.After(max) {
			max = *t
		}
	}

	return max
}
