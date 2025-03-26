package actions

import (
	"fmt"
	"net/url"

	"github.com/flanksource/commons/http"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type HTTPResultResponse struct {
	Headers    map[string]string `json:"headers"`
	StatusCode int               `json:"statusCode"`
}

type HTTPResult struct {
	Response HTTPResultResponse `json:"response"`
	Body     string             `json:"body"`
}

type HTTP struct {
}

func (c *HTTP) Run(ctx context.Context, action v1.HTTPAction) (*HTTPResult, error) {
	var connection = &models.Connection{
		URL: action.URL,
	}

	if action.HTTPConnection.Connection != "" {
		connection, err := pkgConnection.Get(ctx, action.HTTPConnection.Connection)
		if err != nil {
			return nil, fmt.Errorf("failed to hydrate connection: %w", err)
		} else if connection != nil {
			if ntlm, ok := connection.Properties["ntlm"]; ok {
				action.NTLM = ntlm == "true"
			} else if ntlm, ok := connection.Properties["ntlmv2"]; ok {
				action.NTLMv2 = ntlm == "true"
			}
		}
	}

	if connection.URL == "" {
		return nil, fmt.Errorf("must specify a URL")
	} else if _, err := url.Parse(connection.URL); err != nil {
		return nil, fmt.Errorf("failed to parse url(%q): %w", connection.URL, err)
	}

	resp, err := c.makeRequest(ctx, action, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}

	body, err := resp.AsString()
	if err != nil {
		return nil, fmt.Errorf("failed to get response body: %w", err)
	}

	result := &HTTPResult{
		Response: HTTPResultResponse{
			StatusCode: resp.StatusCode,
			Headers:    make(map[string]string),
		},
		Body: body,
	}

	for k, v := range resp.Header {
		result.Response.Headers[k] = v[0]
	}

	return result, nil
}

// makeRequest creates a new HTTP request and makes the HTTP call.
func (c *HTTP) makeRequest(ctx context.Context, action v1.HTTPAction, connection *models.Connection) (*http.Response, error) {
	client := http.NewClient()

	client.NTLM(action.NTLM)
	client.NTLMV2(action.NTLMv2)

	if connection.Username != "" || connection.Password != "" {
		client.Auth(connection.Username, connection.Password)
	}

	req := http.NewClient().R(ctx)

	for _, header := range action.Headers {
		value, err := ctx.GetEnvValueFromCache(header, ctx.GetNamespace())
		if err != nil {
			return nil, fmt.Errorf("failed getting header (%v): %w", header, err)
		}

		req.Header(header.Name, value)
	}

	if action.Method == "" {
		action.Method = "GET"
	}

	if action.Body != "" {
		if err := req.Body(action.Body); err != nil {
			return nil, fmt.Errorf("failed to parse body: %w", err)
		}
	}

	response, err := req.Do(action.Method, connection.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}

	return response, nil
}
