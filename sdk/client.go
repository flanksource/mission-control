package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	icapi "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/pkg/httpobservability"
)

// ErrHTMLResponse is returned when the server responded with HTML on a JSON
// endpoint — typically because the configured server URL points at the
// user-facing frontend rather than the /api backend.
var ErrHTMLResponse = errors.New("server returned HTML instead of JSON (is the backend at /api?)")

type Client struct {
	*http.Client
}

func New(serverURL, token string) *Client {
	return &Client{
		Client: httpobservability.Apply(http.NewClient().
			BaseURL(serverURL).
			Header("Authorization", "Bearer "+token).
			Header("Accept", "application/json").
			Header("Content-Type", "application/json").
			UserAgent("mission-control-cli")),
	}
}

// decodeJSON parses a response body as JSON, returning ErrHTMLResponse if the
// body looks like HTML (frontend served instead of backend JSON).
func decodeJSON(r *http.Response, out any) error {
	body, err := r.AsString()
	if err != nil {
		return err
	}
	if looksLikeHTML(r.Header.Get("Content-Type"), body) {
		return ErrHTMLResponse
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return nil
}

func looksLikeHTML(contentType, body string) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	return strings.HasPrefix(strings.TrimLeft(body, " \t\r\n"), "<")
}

func (c *Client) GetConnection(name, namespace string) (*models.Connection, error) {
	var connections []models.Connection
	r, err := c.R(context.Background()).
		QueryParam("name", "eq."+name).
		QueryParam("namespace", "eq."+namespace).
		QueryParam("deleted_at", "is.null").
		QueryParam("limit", "1").
		Get("/db/connections")
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		return nil, fmt.Errorf("server returned %d", r.StatusCode)
	}
	if err := decodeJSON(r, &connections); err != nil {
		return nil, err
	}
	if len(connections) == 0 {
		return nil, fmt.Errorf("connection %s/%s not found", namespace, name)
	}
	return &connections[0], nil
}

func (c *Client) SaveConnection(conn *models.Connection) error {
	r, err := c.R(context.Background()).
		Header("Prefer", "resolution=merge-duplicates,return=representation").
		Post("/db/connections", conn)
	if err != nil {
		return err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		return fmt.Errorf("server returned %d: %s", r.StatusCode, body)
	}
	return nil
}

type TestResult struct {
	Message string `json:"message"`
	Payload any    `json:"payload"`
}

type PlaybookListOptions struct {
	ConfigID    string
	CheckID     string
	ComponentID string
}

type PlaybookRunParams struct {
	ID          uuid.UUID         `json:"id"`
	ConfigID    *uuid.UUID        `json:"config_id,omitempty"`
	CheckID     *uuid.UUID        `json:"check_id,omitempty"`
	ComponentID *uuid.UUID        `json:"component_id,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
}

type PlaybookRunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

type PlaybookSummary struct {
	Playbook models.Playbook            `json:"playbook,omitempty"`
	Run      models.PlaybookRun         `json:"run,omitempty"`
	Actions  []models.PlaybookRunAction `json:"actions,omitempty"`
}

func (c *Client) TestConnection(id string) (*TestResult, error) {
	var result TestResult
	r, err := c.R(context.Background()).
		Post("/connection/test/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("test failed (%d): %s", r.StatusCode, body)
	}
	if err := decodeJSON(r, &result); err != nil {
		return &result, err
	}
	return &result, nil
}

func (c *Client) ListPlaybooks(opts PlaybookListOptions) ([]icapi.PlaybookListItem, error) {
	var playbooks []icapi.PlaybookListItem
	req := c.R(context.Background())
	if opts.ConfigID != "" {
		req.QueryParam("config_id", opts.ConfigID)
	}
	if opts.CheckID != "" {
		req.QueryParam("check_id", opts.CheckID)
	}
	if opts.ComponentID != "" {
		req.QueryParam("component_id", opts.ComponentID)
	}

	r, err := req.Get("/playbook/list")
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("list playbooks failed (%d): %s", r.StatusCode, body)
	}
	if err := decodeJSON(r, &playbooks); err != nil {
		return nil, err
	}
	return playbooks, nil
}

func (c *Client) RunPlaybook(params PlaybookRunParams) (*PlaybookRunResponse, error) {
	var response PlaybookRunResponse
	r, err := c.R(context.Background()).Post("/playbook/run", params)
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("run playbook failed (%d): %s", r.StatusCode, body)
	}
	if err := decodeJSON(r, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetPlaybookRunStatus(id string) (*PlaybookSummary, error) {
	var summary PlaybookSummary
	r, err := c.R(context.Background()).Get("/playbook/run/" + url.PathEscape(id) + "/status")
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("get playbook run status failed (%d): %s", r.StatusCode, body)
	}
	if err := decodeJSON(r, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}
