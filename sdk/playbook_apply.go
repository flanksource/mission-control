package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
)

// PlaybookApplyParams contains the canonical database fields derived from a Playbook manifest.
type PlaybookApplyParams struct {
	Namespace   string
	Name        string
	Title       string
	Icon        string
	Description string
	Category    string
	Spec        json.RawMessage
}

// PlaybookApplyResult describes whether apply created or updated the playbook.
type PlaybookApplyResult struct {
	Playbook models.Playbook
	Created  bool
}

type playbookWrite struct {
	Namespace   string     `json:"namespace"`
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Icon        string     `json:"icon"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Spec        types.JSON `json:"spec"`
	Source      string     `json:"source,omitempty"`
}

// ApplyPlaybook creates a file-backed playbook or updates a non-Kubernetes playbook with the same identity.
func (c *Client) ApplyPlaybook(ctx context.Context, params PlaybookApplyParams) (*PlaybookApplyResult, error) {
	existing, err := c.findPlaybooksForApply(ctx, params.Namespace, params.Name)
	if err != nil {
		return nil, err
	}

	var target *models.Playbook
	if len(existing) == 1 {
		target = &existing[0]
	} else if len(existing) > 1 {
		for i := range existing {
			if existing[i].Category == params.Category {
				if target != nil {
					return nil, fmt.Errorf("multiple playbooks match %s/%s in category %q", params.Namespace, params.Name, params.Category)
				}
				target = &existing[i]
			}
		}
		if target == nil {
			return nil, fmt.Errorf("multiple playbooks match %s/%s; specify an existing category", params.Namespace, params.Name)
		}
	}

	write := playbookWrite{
		Namespace:   params.Namespace,
		Name:        params.Name,
		Title:       params.Title,
		Icon:        params.Icon,
		Description: params.Description,
		Category:    params.Category,
		Spec:        types.JSON(params.Spec),
	}
	if target == nil {
		write.Source = models.SourceConfigFile
		playbook, err := c.createPlaybook(ctx, write)
		if err != nil {
			return nil, err
		}
		return &PlaybookApplyResult{Playbook: *playbook, Created: true}, nil
	}
	if target.Source == models.SourceCRD {
		return nil, fmt.Errorf("playbook %s/%s is managed by Kubernetes and cannot be applied", target.Namespace, target.Name)
	}

	playbook, err := c.updatePlaybook(ctx, target.ID.String(), write)
	if err != nil {
		return nil, err
	}
	return &PlaybookApplyResult{Playbook: *playbook}, nil
}

func (c *Client) findPlaybooksForApply(ctx context.Context, namespace, name string) ([]models.Playbook, error) {
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
		return nil, playbookWriteError(response)
	}

	var playbooks []models.Playbook
	if err := decodeJSON(response, &playbooks); err != nil {
		return nil, err
	}
	return playbooks, nil
}

func (c *Client) createPlaybook(ctx context.Context, write playbookWrite) (*models.Playbook, error) {
	response, err := c.R(ctx).
		Header("Prefer", "return=representation").
		Post(c.apiPath("/db/playbooks"), write)
	if err != nil {
		return nil, err
	}
	return decodePlaybookWriteResponse(response)
}

func (c *Client) updatePlaybook(ctx context.Context, id string, write playbookWrite) (*models.Playbook, error) {
	response, err := c.R(ctx).
		Header("Prefer", "return=representation").
		QueryParam("id", "eq."+id).
		Patch(c.apiPath("/db/playbooks"), write)
	if err != nil {
		return nil, err
	}
	return decodePlaybookWriteResponse(response)
}

func decodePlaybookWriteResponse(response *http.Response) (*models.Playbook, error) {
	if !response.IsOK() {
		return nil, playbookWriteError(response)
	}

	body, err := response.AsString()
	if err != nil {
		return nil, err
	}
	var playbooks []models.Playbook
	if err := json.Unmarshal([]byte(body), &playbooks); err != nil {
		return nil, fmt.Errorf("failed to decode playbook write response: %w", err)
	}
	if len(playbooks) != 1 {
		return nil, fmt.Errorf("playbook write returned %d records", len(playbooks))
	}
	return &playbooks[0], nil
}

func playbookWriteError(response *http.Response) error {
	body, err := response.AsString()
	if err != nil {
		return err
	}
	if looksLikeHTML(response.Header.Get("Content-Type"), body) {
		return ErrHTMLResponse
	}
	return newServerError(response.StatusCode, []byte(strings.TrimSpace(body)))
}
