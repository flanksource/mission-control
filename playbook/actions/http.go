package actions

import (
	"fmt"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type HTTPResult struct {
	Content    string            `json:"content"`
	Headers    map[string]string `json:"headers"`
	StatusCode int               `json:"code"`
}

type HTTP struct {
}

func (c *HTTP) Run(ctx context.Context, action v1.HTTPAction) (*HTTPResult, error) {
	if action.HTTPConnection.ConnectionName != "" {
		if _, err := connection.Get(ctx, action.HTTPConnection.ConnectionName); err != nil {
			return nil, fmt.Errorf("failed to hydrate connection: %w", err)
		}
	}

	hydrated, err := action.HTTPConnection.Hydrate(ctx, ctx.GetNamespace())
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	}
	action.HTTPConnection = *hydrated

	if action.URL == "" {
		return nil, fmt.Errorf("must specify a URL")
	}

	client, err := connection.CreateHTTPClient(ctx, action.HTTPConnection)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	resp, err := c.makeRequest(ctx, action, client)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}

	body, err := resp.AsString()
	if err != nil {
		return nil, fmt.Errorf("failed to get response body: %w", err)
	}

	result := &HTTPResult{
		StatusCode: resp.StatusCode,
		Headers:    make(map[string]string),
		Content:    body,
	}

	for k, v := range resp.Header {
		result.Headers[k] = v[0]
	}

	return result, nil
}

func (c *HTTP) makeRequest(ctx context.Context, action v1.HTTPAction, client *http.Client) (*http.Response, error) {
	req := client.R(ctx)

	if action.Method == "" {
		action.Method = "GET"
	}

	if action.Body != "" {
		if err := req.Body(action.Body); err != nil {
			return nil, fmt.Errorf("failed to parse body: %w", err)
		}
	}

	return req.Do(action.Method, action.URL)
}
