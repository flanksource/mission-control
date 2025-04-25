package cloudwatch

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/utils"
	"github.com/samber/lo"
)

type Searcher struct {
	client *cloudwatchlogs.Client
}

func NewSearcher(client *cloudwatchlogs.Client) *Searcher {
	return &Searcher{
		client: client,
	}
}

func (t *Searcher) Search(ctx context.Context, request Request) (*logs.LogResult, error) {
	searchQuery := &cloudwatchlogs.StartQueryInput{
		LogGroupName: &request.LogGroup,
		QueryString:  &request.Query,
	}

	if request.Limit != "" {
		limit, err := strconv.Atoi(request.Limit)
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
	}

	for _, fields := range queryResult.Results {
		var line = logs.LogLine{
			Labels: make(map[string]string),
		}

		for _, field := range fields {
			switch lo.FromPtr(field.Field) {
			case "@message":
				line.Message = lo.FromPtr(field.Value)
			case "@timestamp":
				line.FirstObserved = lo.FromPtr(utils.ParseTime(lo.FromPtr(field.Value)))
			case "@ptr": // the value to use as logRecordPointer to retrieve that complete log event record.
				line.ID = lo.FromPtr(field.Value)
			case "@logStream":
				line.Source = lo.FromPtr(field.Value)
			case "", "@log":
				// Do nothing
			default:
				line.Labels[lo.FromPtr(field.Field)] = lo.FromPtr(field.Value)
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
