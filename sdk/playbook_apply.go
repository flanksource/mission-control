package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strings"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/incident-commander/clientapi"
)

type PlaybookApplyParams = clientapi.PlaybookApplyRequest
type PlaybookApplyResult = clientapi.PlaybookApplyResponse

type playbookWrite struct {
	Namespace   string          `json:"namespace"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Icon        string          `json:"icon"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Spec        json.RawMessage `json:"spec"`
	Source      string          `json:"source,omitempty"`
}

// ApplyPlaybook applies through the server API, falling back to the legacy PostgREST flow during rolling upgrades.
func (c *Client) ApplyPlaybook(ctx context.Context, params PlaybookApplyParams) (*PlaybookApplyResult, error) {
	response, err := c.R(ctx).Post(c.apiPath("/playbook/apply"), params)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == stdhttp.StatusNotFound {
		_, _ = response.AsString()
		return c.applyPlaybookLegacy(ctx, params)
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

func (c *Client) applyPlaybookLegacy(ctx context.Context, params PlaybookApplyParams) (*PlaybookApplyResult, error) {
	var spec clientapi.PlaybookSpecSummary
	if err := json.Unmarshal(params.Spec, &spec); err != nil {
		return nil, fmt.Errorf("decode playbook spec for legacy apply: %w", err)
	}

	existing, err := c.findPlaybooksForApply(ctx, params.Namespace, params.Name)
	if err != nil {
		return nil, err
	}

	var target *clientapi.Playbook
	if len(existing) == 1 {
		target = &existing[0]
	} else if len(existing) > 1 {
		for i := range existing {
			if existing[i].Category == spec.Category {
				if target != nil {
					return nil, fmt.Errorf("multiple playbooks match %s/%s in category %q", params.Namespace, params.Name, spec.Category)
				}
				target = &existing[i]
			}
		}
		if target == nil {
			return nil, fmt.Errorf("multiple playbooks match %s/%s; specify an existing category", params.Namespace, params.Name)
		}
	}

	title := spec.Title
	if title == "" {
		title = params.Name
	}
	write := playbookWrite{
		Namespace:   params.Namespace,
		Name:        params.Name,
		Title:       title,
		Icon:        spec.Icon,
		Description: spec.Description,
		Category:    spec.Category,
		Spec:        params.Spec,
	}
	if target == nil {
		write.Source = clientapi.SourceUI
		playbook, err := c.createPlaybook(ctx, write)
		if err != nil {
			return nil, err
		}
		return &PlaybookApplyResult{Playbook: *playbook, Created: true}, nil
	}
	if target.Source != clientapi.SourceUI {
		return nil, fmt.Errorf("playbook %s/%s was not created through the API and cannot be applied", target.Namespace, target.Name)
	}

	playbook, err := c.updatePlaybook(ctx, target.ID.String(), write)
	if err != nil {
		return nil, err
	}
	return &PlaybookApplyResult{Playbook: *playbook}, nil
}

func (c *Client) findPlaybooksForApply(ctx context.Context, namespace, name string) ([]clientapi.Playbook, error) {
	response, err := c.R(ctx).
		QueryParam("namespace", "eq."+namespace).
		QueryParam("name", "eq."+name).
		QueryParam("deleted_at", "is.null").
		QueryParam("select", "*").
		Get(c.apiPath("/db/playbooks"))
	if err != nil {
		return nil, err
	}
	if !response.IsOK() {
		return nil, postgrestError(response)
	}

	var playbooks []clientapi.Playbook
	if err := decodeJSON(response, &playbooks); err != nil {
		return nil, err
	}
	return playbooks, nil
}

func (c *Client) createPlaybook(ctx context.Context, write playbookWrite) (*clientapi.Playbook, error) {
	response, err := c.R(ctx).
		Header("Prefer", "return=representation").
		Post(c.apiPath("/db/playbooks"), write)
	if err != nil {
		return nil, err
	}
	return decodePlaybookWriteResponse(response)
}

func (c *Client) updatePlaybook(ctx context.Context, id string, write playbookWrite) (*clientapi.Playbook, error) {
	response, err := c.R(ctx).
		Header("Prefer", "return=representation").
		QueryParam("id", "eq."+id).
		Patch(c.apiPath("/db/playbooks"), write)
	if err != nil {
		return nil, err
	}
	return decodePlaybookWriteResponse(response)
}

func decodePlaybookWriteResponse(response *http.Response) (*clientapi.Playbook, error) {
	if !response.IsOK() {
		return nil, postgrestError(response)
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
