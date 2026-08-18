// Package sdk preserves the public Mission Control SDK while Faro uses the
// lightweight transport in sdk/client directly.
package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strings"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"

	icapi "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/clientapi"
	lean "github.com/flanksource/incident-commander/sdk/client"
)

var (
	ErrHTMLResponse = lean.ErrHTMLResponse
	ErrNotFound     = lean.ErrNotFound
)

// IsNotFound reports whether err represents a missing resource.
func IsNotFound(err error) bool {
	return lean.IsNotFound(err)
}

type TokenProvider func(context.Context) (string, error)

type ClientOption func(*Client)

// Client retains the original SDK surface while delegating wire operations to the lean client.
type Client struct {
	*http.Client
	serverURL     string
	tokenProvider TokenProvider
	lean          *lean.Client
}

func New(serverURL, token string, opts ...ClientOption) *Client {
	authHeader := ""
	if token != "" {
		authHeader = "Bearer " + token
	}
	return NewWithAuthHeader(serverURL, authHeader, opts...)
}

// NewWithAuthHeader returns a client using the provided Authorization header.
func NewWithAuthHeader(serverURL, authHeader string, opts ...ClientOption) *Client {
	leanClient := lean.NewWithAuthHeader(serverURL, authHeader)
	out := &Client{
		Client:    leanClient.Client,
		serverURL: strings.TrimRight(serverURL, "/"),
		lean:      leanClient,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(out)
		}
	}
	if out.tokenProvider != nil {
		out.Client = out.Client.Use(tokenProviderMiddleware(out.tokenProvider))
	}
	out.lean.Client = out.Client
	return out
}

func (c *Client) leanClient() *lean.Client {
	return c.lean
}

func WithTokenProvider(provider TokenProvider) ClientOption {
	return func(c *Client) {
		c.tokenProvider = provider
	}
}

func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) {
		if c.Client != nil && userAgent != "" {
			c.Client.UserAgent(userAgent)
		}
	}
}

func WithAccept(accept string) ClientOption {
	return func(c *Client) {
		if c.Client != nil && accept != "" {
			c.Client.Header("Accept", accept)
		}
	}
}

func tokenProviderMiddleware(provider TokenProvider) func(stdhttp.RoundTripper) stdhttp.RoundTripper {
	return func(next stdhttp.RoundTripper) stdhttp.RoundTripper {
		return roundTripperFunc(func(req *stdhttp.Request) (*stdhttp.Response, error) {
			token, err := provider(req.Context())
			if err != nil {
				return nil, err
			}
			if token != "" {
				req = req.Clone(req.Context())
				req.Header.Set("Authorization", "Bearer "+token)
			}
			return next.RoundTrip(req)
		})
	}
}

type roundTripperFunc func(*stdhttp.Request) (*stdhttp.Response, error)

func (f roundTripperFunc) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	return f(req)
}

func (c *Client) apiPath(path string) string {
	if strings.HasSuffix(c.serverURL, "/api") && strings.HasPrefix(path, "/api/") {
		return strings.TrimPrefix(path, "/api")
	}
	return path
}

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

type ServerError = lean.ServerError
type WhoamiResponse = lean.WhoamiResponse

func (c *Client) Whoami(ctx context.Context) (*WhoamiResponse, int, error) {
	return c.leanClient().Whoami(ctx)
}

func (c *Client) GetConnection(name, namespace string) (*models.Connection, error) {
	connection, err := c.leanClient().GetConnection(name, namespace)
	if err != nil {
		return nil, err
	}
	var out models.Connection
	if err := convertJSON(connection, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SaveConnection(connection *models.Connection) error {
	var dto clientapi.Connection
	if err := convertJSON(connection, &dto); err != nil {
		return err
	}
	return c.leanClient().SaveConnection(&dto)
}

func (c *Client) ListPluginRPCServices(ctx context.Context) ([]rpc.RPCService, error) {
	services, err := c.leanClient().ListPluginRPCServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []rpc.RPCService
	if err := convertJSON(services, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DispatchPluginOperation(ctx context.Context, plugin, operation string, params []byte, configID string) ([]byte, int, error) {
	return c.leanClient().DispatchPluginOperation(ctx, plugin, operation, params, configID)
}

type TestResult = lean.TestResult
type PlaybookListOptions = lean.PlaybookListOptions
type PlaybookRunParams = lean.PlaybookRunParams
type PlaybookRunResponse = lean.PlaybookRunResponse

type PlaybookSummary struct {
	Playbook models.Playbook            `json:"playbook,omitempty"`
	Run      models.PlaybookRun         `json:"run,omitempty"`
	Actions  []models.PlaybookRunAction `json:"actions,omitempty"`
}

func (c *Client) TestConnection(id string) (*TestResult, error) {
	return c.leanClient().TestConnection(id)
}

// InvokePluginOperation invokes a plugin operation through the Mission Control HTTP API.
func (c *Client) InvokePluginOperation(name, operation, configID string, params json.RawMessage) ([]byte, error) {
	return c.leanClient().InvokePluginOperation(name, operation, configID, params)
}

func (c *Client) ListPlaybooks(opts PlaybookListOptions) ([]icapi.PlaybookListItem, error) {
	playbooks, err := c.leanClient().ListPlaybooks(opts)
	if err != nil {
		return nil, err
	}
	var out []icapi.PlaybookListItem
	if err := convertJSON(playbooks, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) RunPlaybook(params PlaybookRunParams) (*PlaybookRunResponse, error) {
	return c.leanClient().RunPlaybook(params)
}

func (c *Client) GetPlaybookRunStatus(id string) (*PlaybookSummary, error) {
	summary, err := c.leanClient().GetPlaybookRunStatus(id)
	if err != nil {
		return nil, err
	}
	var out PlaybookSummary
	if err := convertJSON(summary, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func convertJSON(in, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
