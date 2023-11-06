package actions

import (
	"fmt"
	netHTTP "net/http"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type HTTPResult struct {
	Code    string
	Headers netHTTP.Header
	Body    string
}

type HTTP struct {
}

func (c *HTTP) Run(ctx context.Context, action v1.HTTPAction, env TemplateEnv) (*HTTPResult, error) {
	connection, err := ctx.HydrateConnectionByURL(action.HTTPConnection.Connection)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	} else if connection != nil {
		if ntlm, ok := connection.Properties["ntlm"]; ok {
			action.NTLM = ntlm == "true"
		} else if ntlm, ok := connection.Properties["ntlmv2"]; ok {
			action.NTLMv2 = ntlm == "true"
		}

		if _, err := url.Parse(connection.URL); err != nil {
			return nil, fmt.Errorf("failed to parse url(%q): %w", connection.URL, err)
		}
	} else if connection == nil {
		connection = &models.Connection{
			URL: action.URL,
		}
	}

	if connection.URL == "" {
		return nil, fmt.Errorf("must specify a URL")
	}

	if action.TemplateBody {
		templated, err := gomplate.RunTemplate(env.AsMap(), gomplate.Template{Template: action.Body})
		if err != nil {
			return nil, err
		}

		action.Body = templated
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
		Code:    resp.Status,
		Headers: resp.Header,
		Body:    body,
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
