package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strings"

	"github.com/flanksource/clicky/rpc"
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
	serverURL     string
	tokenProvider TokenProvider
}

func New(serverURL, token string) *Client {
	return NewWithAuthHeader(serverURL, "Bearer "+token)
}

// NewWithAuthHeader returns a client using the provided Authorization header.
func NewWithAuthHeader(serverURL, authHeader string) *Client {
	client := http.NewClient().
		BaseURL(serverURL).
		Header("Content-Type", "application/json").
		UserAgent("mission-control-cli")
	if authHeader != "" {
		client = client.Header("Authorization", authHeader)
	}
	return &Client{Client: httpobservability.Apply(client)}
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

func newServerError(statusCode int, body []byte) *ServerError {
	err := &ServerError{
		StatusCode: statusCode,
		Body:       append([]byte(nil), body...),
	}
	var payload struct {
		Code       any            `json:"code"`
		Error      string         `json:"error"`
		Message    string         `json:"message"`
		Trace      string         `json:"trace"`
		Time       any            `json:"time"`
		Context    map[string]any `json:"context"`
		Hint       string         `json:"hint"`
		Public     string         `json:"public"`
		Stacktrace string         `json:"stacktrace"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return err
	}
	err.Code = stringifyServerErrorField(payload.Code)
	err.Message = payload.Error
	if err.Message == "" {
		err.Message = payload.Message
	}
	err.Trace = payload.Trace
	err.Time = stringifyServerErrorField(payload.Time)
	err.Context = payload.Context
	err.Hint = payload.Hint
	err.Public = payload.Public
	err.Stacktrace = payload.Stacktrace
	return err
}

func stringifyServerErrorField(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

type WhoamiResponse struct {
	Payload struct {
		User  map[string]any `json:"user"`
		Roles []string       `json:"roles"`
	} `json:"payload"`
}

func (c *Client) Whoami(ctx context.Context) (*WhoamiResponse, int, error) {
	var result WhoamiResponse
	r, err := c.R(ctx).Get("/auth/whoami")
	if err != nil {
		return nil, 0, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, r.StatusCode, ErrHTMLResponse
		}
		return nil, r.StatusCode, fmt.Errorf("whoami failed (%d): %s", r.StatusCode, strings.TrimSpace(body))
	}
	if err := decodeJSON(r, &result); err != nil {
		return nil, r.StatusCode, err
	}
	return &result, r.StatusCode, nil
}

func (c *Client) ProbeAPIBase(ctx context.Context) (bool, error) {
	r, err := c.R(ctx).
		QueryParam("limit", "0").
		Get(c.apiPath("/api/db/connections"))
	if err != nil {
		return false, err
	}
	body, err := r.AsString()
	if err != nil {
		return false, err
	}
	switch r.StatusCode {
	case 200, 401, 403:
	default:
		return false, nil
	}
	if looksLikeHTML(r.Header.Get("Content-Type"), body) {
		return false, nil
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	trimmed := strings.TrimLeft(body, " \t\r\n")
	return strings.Contains(contentType, "json") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{"), nil
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

type pluginRPCListItem struct {
	Name    string         `json:"name"`
	Service rpc.RPCService `json:"service"`
}

func (c *Client) ListPluginRPCServices(ctx context.Context) ([]rpc.RPCService, error) {
	resp, err := c.R(ctx).
		QueryParam("format", "clicky-rpc").
		Get(c.apiPath("/api/plugins"))
	if err != nil {
		return nil, fmt.Errorf("GET /api/plugins: %w", err)
	}
	if !resp.IsOK() {
		body, _ := resp.AsString()
		if looksLikeHTML(resp.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, body)
	}
	body, err := resp.AsString()
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var items []pluginRPCListItem
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		return nil, fmt.Errorf("decode listing: %w", err)
	}
	out := make([]rpc.RPCService, 0, len(items))
	for _, it := range items {
		svc := it.Service
		if svc.Name == "" {
			svc.Name = it.Name
		}
		out = append(out, svc)
	}
	return out, nil
}

func (c *Client) DispatchPluginOperation(ctx context.Context, plugin, op string, params []byte, configID string) ([]byte, int, error) {
	req := c.R(ctx).Header("Content-Type", "application/json")
	if configID != "" {
		req = req.QueryParam("config_id", configID)
	}
	resp, err := req.Post(c.apiPath(fmt.Sprintf("/api/plugins/%s/operations/%s", plugin, op)), params)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		if looksLikeHTML(resp.Header.Get("Content-Type"), string(body)) {
			return body, resp.StatusCode, ErrHTMLResponse
		}
		return body, resp.StatusCode, newServerError(resp.StatusCode, body)
	}
	return body, resp.StatusCode, nil
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

// InvokePluginOperation invokes a plugin operation through the Mission Control HTTP API.
func (c *Client) InvokePluginOperation(name, operation, configID string, params json.RawMessage) ([]byte, error) {
	req := c.R(context.Background())
	if configID != "" {
		req = req.QueryParam("config_id", configID)
	}

	r, err := req.Post("/api/plugins/"+url.PathEscape(name)+"/invoke/"+url.PathEscape(operation), params)
	if err != nil {
		return nil, err
	}

	body, err := r.AsString()
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		return nil, fmt.Errorf("plugin %s/%s failed (%d): %s", name, operation, r.StatusCode, body)
	}
	return []byte(body), nil
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
