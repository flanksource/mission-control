package cloudwatch

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
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

func (t *Searcher) Search(ctx context.Context, request Request) (*cloudWatchResult, error) {
	searchQuery := &cloudwatchlogs.StartQueryInput{
		LogGroupName: &request.LogGroup,
		QueryString:  &request.Query,
	}

	if request.Limit > 0 {
		searchQuery.Limit = lo.ToPtr(request.Limit)
	}

	if request.GetStart() != nil {
		searchQuery.StartTime = lo.ToPtr(request.GetStart().UnixMilli())
	}

	if request.GetEnd() != nil {
		searchQuery.EndTime = lo.ToPtr(request.GetEnd().UnixMilli())
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

	var result cloudWatchResult
	result.Total = int(queryResult.Statistics.RecordsMatched)
	result.Events = make([]Event, 0, len(queryResult.Results))
	for _, fields := range queryResult.Results {
		var event = Event{
			Labels: make(map[string]string),
		}

		for _, field := range fields {
			switch lo.FromPtr(field.Field) {
			case "@message":
				event.Message = lo.FromPtr(field.Value)
			case "@timestamp":
				event.Time = toRFC339(lo.FromPtr(field.Value))
			case "@ptr": // the value to use as logRecordPointer to retrieve that complete log event record.
				event.ID = lo.FromPtr(field.Value)
			case "":
				// Do nothing
			default:
				event.Labels[lo.FromPtr(field.Field)] = lo.FromPtr(field.Value)
			}
		}

		result.Events = append(result.Events, event)
	}

	return &result, nil
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

// timestamp layout returned by Cloudwatch
const timestampLayout = "2006-01-02 15:04:05.000"

// Converts the timestamp returned by Cloudwatch
// to RFC3339 format.
func toRFC339(input string) string {
	t, err := time.Parse(timestampLayout, input)
	if err != nil {
		return ""
	}

	return t.Format(time.RFC3339)
}
