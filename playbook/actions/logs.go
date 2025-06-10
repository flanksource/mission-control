package actions

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"

	"github.com/flanksource/duty/logs"
	"github.com/flanksource/duty/logs/cloudwatch"
	"github.com/flanksource/duty/logs/k8s"
	"github.com/flanksource/duty/logs/loki"
	"github.com/flanksource/duty/logs/opensearch"
	v1 "github.com/flanksource/incident-commander/api/v1"
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

	if action.Kubernetes != nil {
		searcher := k8s.NewK8sLogsFetcher(action.Kubernetes.KubernetesConnection)
		response, err := searcher.Fetch(ctx, action.Kubernetes.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs from kubernetes")
		}

		result := &logsResult{
			Metadata: map[string]any{},
			Logs:     []*logs.LogLine{},
		}

		for _, logGroup := range response {
			result.Logs = append(result.Logs, postProcessLogs(ctx, logGroup.Logs, action.Kubernetes.LogsPostProcess)...)
		}

		return result, nil
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

// dedupLogs consolidates log lines by matching all specified fields together.
func dedupLogs(logLines []*logs.LogLine, dedupe *v1.LogDedupe) []*logs.LogLine {
	if dedupe == nil {
		return logLines
	}

	sort.Slice(logLines, func(i, j int) bool {
		return logLines[i].FirstObserved.Before(logLines[j].FirstObserved)
	})

	if dedupe.Window != "" {
		window, err := duration.ParseDuration(dedupe.Window)
		if err != nil {
			return logLines
		}

		windowedLogs := divideLogsByWindow(logLines, time.Duration(window))

		dedupedLogs := make([]*logs.LogLine, 0, len(logLines))
		for _, windowedLogs := range windowedLogs {
			dedupedLogs = append(dedupedLogs, dedupeWindow(windowedLogs, dedupe.Fields)...)
		}

		return dedupedLogs
	}

	return dedupeWindow(logLines, dedupe.Fields)
}

func divideLogsByWindow(logLines []*logs.LogLine, window time.Duration) [][]*logs.LogLine {
	logsByWindow := make([][]*logs.LogLine, 0, len(logLines))

	var currentWindowStart time.Time
	var currentWindow []*logs.LogLine

	for _, logLine := range logLines {
		logWindow := logLine.FirstObserved.Truncate(window)
		if currentWindowStart.IsZero() {
			currentWindowStart = logWindow
			currentWindow = append(currentWindow, logLine)
			continue
		}

		if logWindow.Equal(currentWindowStart) {
			currentWindow = append(currentWindow, logLine)
			continue
		}

		// start of a new window
		logsByWindow = append(logsByWindow, currentWindow)
		currentWindow = []*logs.LogLine{logLine}
		currentWindowStart = logWindow
	}

	if len(currentWindow) > 0 {
		logsByWindow = append(logsByWindow, currentWindow)
	}

	return logsByWindow
}

// dedupeWindow deduplicates log lines in a given time window.
// all logs provided are expected to be in the same time window.
func dedupeWindow(logLines []*logs.LogLine, fields []string) []*logs.LogLine {
	dedupedLogs := make([]*logs.LogLine, 0, len(logLines))
	seen := make(map[string]*logs.LogLine)

	for _, logLine := range logLines {
		key := logLine.GetDedupKey(fields...)

		previous, found := seen[key]
		if !found {
			seen[key] = logLine
			dedupedLogs = append(dedupedLogs, logLine)
			continue
		}

		previous.Count += logLine.Count
		if logLine.FirstObserved.Before(previous.FirstObserved) {
			previous.FirstObserved = logLine.FirstObserved
		}

		if logLine.LastObserved != nil {
			if previous.LastObserved == nil || logLine.LastObserved.After(*previous.LastObserved) {
				previous.LastObserved = logLine.LastObserved
			}
		} else if !logLine.FirstObserved.IsZero() {
			previous.LastObserved = &logLine.FirstObserved
		}

		// Use the values from the latest log
		previous.Message = logLine.Message
		previous.Host = logLine.Host
		previous.Severity = logLine.Severity
		previous.Source = logLine.Source
	}

	return dedupedLogs
}

func matchLogs(ctx context.Context, logLines []*logs.LogLine, matchExprs []types.MatchExpression) []*logs.LogLine {
	if len(matchExprs) == 0 {
		return logLines
	}

	faultyExpressions := make(map[string]struct{})
	var matchedLogs []*logs.LogLine

outer:
	for _, logLine := range logLines {
		env := logLine.TemplateContext()

		for _, matchExpr := range matchExprs {
			if _, ok := faultyExpressions[string(matchExpr)]; ok {
				continue
			}

			expr := gomplate.Template{Expression: string(matchExpr)}
			ok, err := gomplate.RunTemplateBool(env, expr)
			if err != nil {
				ctx.Logger.V(4).Infof("failed to evaluate match expression '%s': %v", matchExprs, err)
				faultyExpressions[string(matchExpr)] = struct{}{}
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
