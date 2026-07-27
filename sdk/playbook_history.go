package sdk

import (
	"context"
	"strconv"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

// PlaybookRunListOptions filters playbook execution history.
type PlaybookRunListOptions struct {
	PlaybookID *uuid.UUID
	Statuses   []models.PlaybookRunStatus
	Limit      int
}

// ListPlaybookRuns returns top-level playbook runs in reverse chronological order.
func (c *Client) ListPlaybookRuns(ctx context.Context, opts PlaybookRunListOptions) ([]models.PlaybookRun, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	request := c.R(ctx).
		QueryParam("parent_id", "is.null").
		QueryParam("order", "created_at.desc").
		QueryParam("limit", strconv.Itoa(limit)).
		QueryParam("select", "*")
	if opts.PlaybookID != nil {
		request = request.QueryParam("playbook_id", "eq."+opts.PlaybookID.String())
	}
	if len(opts.Statuses) > 0 {
		statuses := make([]string, len(opts.Statuses))
		for i, status := range opts.Statuses {
			statuses[i] = string(status)
		}
		request = request.QueryParam("status", "in.("+strings.Join(statuses, ",")+")")
	}

	response, err := request.Get(c.apiPath("/db/playbook_runs"))
	if err != nil {
		return nil, err
	}
	if !response.IsOK() {
		return nil, postgrestError(response)
	}

	var runs []models.PlaybookRun
	if err := decodeJSON(response, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}
