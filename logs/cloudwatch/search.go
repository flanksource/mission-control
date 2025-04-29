package cloudwatch

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/flanksource/incident-commander/logs"
	"github.com/samber/lo"
)

type Searcher struct {
	client        *cloudwatchlogs.Client
	mappingConfig *logs.FieldMappingConfig
}

func NewSearcher(client *cloudwatchlogs.Client, mappingConfig *logs.FieldMappingConfig) *Searcher {
	return &Searcher{
		client:        client,
		mappingConfig: mappingConfig,
	}
}

func (t *Searcher) Search(ctx context.Context, request Request) (*logs.LogResult, error) {
	searchQuery := &cloudwatchlogs.StartQueryInput{
		LogGroupName: &request.LogGroup,
		QueryString:  &request.Query,
	}

	if request.Limit != "" {
		limit, err := strconv.ParseInt(request.Limit, 10, 32)
		if err != nil {
			return nil, err
		}
		searchQuery.Limit = lo.ToPtr(int32(limit))
	}

	if s, err := request.GetStart(); err == nil {
		searchQuery.StartTime = lo.ToPtr(s.UnixMilli())
	}

	if e, err := request.GetEnd(); err == nil {
		searchQuery.EndTime = lo.ToPtr(e.UnixMilli())
	} else {
		searchQuery.EndTime = lo.ToPtr(time.Now().UnixMilli()) // end time is a required field
	}

	queryOutput, err := t.client.StartQuery(ctx, searchQuery)
	if err != nil {
		return nil, err
	}

	queryResult, err := t.getQueryResults(ctx, queryOutput.QueryId)
	if err != nil {
		return nil, err
	}

	logResult := logs.LogResult{
		Metadata: map[string]any{
			"total":          int(queryResult.Statistics.RecordsMatched),
			"statistics":     queryResult.Statistics,
			"resultMetadata": queryResult.ResultMetadata,
		},
		Logs: make([]logs.LogLine, 0, len(queryResult.Results)),
	}

	mappingConfig := DefaultFieldMappingConfig()
	if t.mappingConfig != nil {
		mappingConfig = t.mappingConfig.WithDefaults(DefaultFieldMappingConfig())
	}

	for _, fields := range queryResult.Results {
		var line logs.LogLine
		for _, field := range fields {
			key := lo.FromPtr(field.Field)
			value := lo.FromPtr(field.Value)

			if err := logs.MapFieldToLogLine(key, value, &line, mappingConfig); err != nil {
				return nil, fmt.Errorf("error mapping field %s: %w", key, err)
			}
		}

		logResult.Logs = append(logResult.Logs, line)
	}

	return &logResult, nil
}

func (t *Searcher) getQueryResults(ctx context.Context, queryID *string) (*cloudwatchlogs.GetQueryResultsOutput, error) {
	input := &cloudwatchlogs.GetQueryResultsInput{
		QueryId: queryID,
	}

	for {
		resp, err := t.client.GetQueryResults(ctx, input)
		if err != nil {
			return nil, err
		}

		switch resp.Status {
		case types.QueryStatusComplete:
			return resp, nil
		case types.QueryStatusFailed:
			return nil, fmt.Errorf("query failed")
		case types.QueryStatusTimeout:
			return nil, fmt.Errorf("query timedout")
		case types.QueryStatusCancelled:
			return nil, fmt.Errorf("query cancelled")
		default:
			// Might be scheduling or running.
			// Wait before retrying.
			time.Sleep(time.Second)
		}
	}
}

func DefaultFieldMappingConfig() logs.FieldMappingConfig {
	return logs.FieldMappingConfig{
		ID:        []string{"@ptr"},
		Ignore:    []string{"@log", ""},
		Source:    []string{"@logStream"},
		Message:   []string{"@message"},
		Timestamp: []string{"@timestamp"},
	}
}
