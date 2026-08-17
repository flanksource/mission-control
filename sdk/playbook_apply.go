package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/incident-commander/clientapi"
	http "github.com/flanksource/incident-commander/clienthttp"
)

type PlaybookApplyParams = clientapi.PlaybookApplyRequest
type PlaybookApplyResult = clientapi.PlaybookApplyResponse

// ApplyPlaybook creates an API-owned playbook or updates an API-owned playbook with the same identity.
func (c *Client) ApplyPlaybook(ctx context.Context, params PlaybookApplyParams) (*PlaybookApplyResult, error) {
	response, err := c.R(ctx).Post(c.apiPath("/playbook/apply"), params)
	if err != nil {
		return nil, err
	}
	if !response.IsOK() {
		return nil, postgrestError(response)
	}
	var result PlaybookApplyResult
	if err := decodeJSON(response, &result); err != nil {
		return nil, fmt.Errorf("decode playbook apply response: %w", err)
	}
	return &result, nil
}

func decodePlaybookWriteResponse(response *http.Response) (*clientapi.Playbook, error) {
	if !response.IsOK() {
		body, err := response.AsString()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("playbook write failed: %s", body)
	}

	body, err := response.AsString()
	if err != nil {
		return nil, err
	}
	var playbooks []clientapi.Playbook
	if err := json.Unmarshal([]byte(body), &playbooks); err != nil {
		return nil, fmt.Errorf("failed to decode playbook write response: %w", err)
	}
	if len(playbooks) != 1 {
		return nil, fmt.Errorf("playbook write returned %d records", len(playbooks))
	}
	return &playbooks[0], nil
}

func postgrestError(response *http.Response) error {
	body, err := response.AsString()
	if err != nil {
		return err
	}
	if looksLikeHTML(response.Header.Get("Content-Type"), body) {
		return ErrHTMLResponse
	}
	return newServerError(response.StatusCode, []byte(strings.TrimSpace(body)))
}
