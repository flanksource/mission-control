package client

import (
	"context"
	"time"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
)

// DeletePlaybook soft-deletes an API-owned playbook.
func (c *Client) DeletePlaybook(ctx context.Context, id uuid.UUID) (*clientapi.Playbook, error) {
	response, err := c.R(ctx).
		Header("Prefer", "return=representation").
		QueryParam("id", "eq."+id.String()).
		Patch(c.apiPath("/db/playbooks"), map[string]time.Time{"deleted_at": time.Now().UTC()})
	if err != nil {
		return nil, err
	}
	return decodePlaybookWriteResponse(response)
}
