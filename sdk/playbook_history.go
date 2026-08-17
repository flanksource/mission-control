package sdk

import (
	"context"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/clientapi"
	lean "github.com/flanksource/incident-commander/sdk/client"
)

// PlaybookRunListOptions filters playbook execution history.
type PlaybookRunListOptions struct {
	PlaybookID *uuid.UUID
	Statuses   []models.PlaybookRunStatus
	Limit      int
}

// ListPlaybookRuns returns top-level playbook runs in reverse chronological order.
func (c *Client) ListPlaybookRuns(ctx context.Context, opts PlaybookRunListOptions) ([]models.PlaybookRun, error) {
	leanOptions := lean.PlaybookRunListOptions{PlaybookID: opts.PlaybookID, Limit: opts.Limit}
	for _, status := range opts.Statuses {
		leanOptions.Statuses = append(leanOptions.Statuses, clientapiPlaybookRunStatus(status))
	}
	runs, err := c.leanClient().ListPlaybookRuns(ctx, leanOptions)
	if err != nil {
		return nil, err
	}
	var out []models.PlaybookRun
	if err := convertJSON(runs, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func clientapiPlaybookRunStatus(status models.PlaybookRunStatus) clientapi.PlaybookRunStatus {
	return clientapi.PlaybookRunStatus(status)
}
