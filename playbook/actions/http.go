package actions

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
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

func (r HTTPResult) String() string  { return r.plain(false) }
func (r HTTPResult) ANSI() string    { return r.plain(true) }
func (r HTTPResult) HTML() string    { return "<pre>" + r.plain(false) + "</pre>" }
func (r HTTPResult) Markdown() string { return "```\n" + r.plain(false) + "\n```" }

func (r HTTPResult) plain(colors bool) string {
	var b strings.Builder

	statusLabel := fmt.Sprintf("Status: %d", r.StatusCode)
	if colors {
		style := "text-green-600"
		if r.StatusCode >= 400 {
			style = "text-red-600"
		}
		b.WriteString(clicky.Text(statusLabel, "font-bold "+style).ANSI())
	} else {
		b.WriteString(statusLabel)
	}
	b.WriteString("\n")

	if len(r.Headers) > 0 {
		keys := make([]string, 0, len(r.Headers))
		for k := range r.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, r.Headers[k]))
		}
	}

	if r.Content != "" {
		b.WriteString("\n")
		b.WriteString(r.Content)
	}

	return strings.TrimRight(b.String(), "\n")
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
