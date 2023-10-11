package actions

import (
	"fmt"
	netHTTP "net/http"
	"net/url"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type HTTPResult struct {
	Code    string
	Headers netHTTP.Header
	Body    string
	SslAge  *time.Duration
}

type HTTP struct {
}

func (c *HTTP) Run(ctx api.Context, action v1.HTTPAction, env TemplateEnv) (*HTTPResult, error) {
	if action.URL == "" {
		return nil, fmt.Errorf("must specify URL")
	}

	connection, err := ctx.HydrateConnection(action.HTTPConnection.Connection)
	if err != nil {
		return nil, fmt.Errorf("must specify URL")
	} else if connection != nil {
		if connection.URL == "" {
			return nil, fmt.Errorf("no url or connection specified")
		}

		if ntlm, ok := connection.Properties["ntlm"]; ok {
			action.NTLM = ntlm == "true"
		} else if ntlm, ok := connection.Properties["ntlmv2"]; ok {
			action.NTLMv2 = ntlm == "true"
		}

		if _, err := url.Parse(connection.URL); err != nil {
			return nil, fmt.Errorf("failed to parse url: %w", err)
		}
	} else if connection == nil {
		connection = &models.Connection{
			URL: action.URL,
		}
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
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}

	body, _ := resp.AsString()
	result := &HTTPResult{
		Code:    resp.Status,
		Headers: resp.Header,
		Body:    body,
		SslAge:  resp.GetSSLAge(),
	}

	return result, nil
}

// makeRequest creates a new HTTP request and makes the HTTP call.
func (c *HTTP) makeRequest(ctx api.Context, action v1.HTTPAction, connection *models.Connection) (*http.Response, error) {
	client := http.NewClient()

	client.NTLM(action.NTLM)
	client.NTLMV2(action.NTLMv2)

	if action.ThresholdMillis > 0 {
		client.Timeout(time.Duration(action.ThresholdMillis) * time.Millisecond)
	}

	if connection.Username != "" || connection.Password != "" {
		client.Auth(connection.Username, connection.Password)
	}

	req := http.NewClient().R(ctx)

	for _, header := range action.Headers {
		value, err := ctx.GetEnvValueFromCache(header)
		if err != nil {
			return nil, fmt.Errorf("failed getting header (%v): %w", header, err)
		}

		req.Header(header.Name, value)
	}

	if action.Method == "" {
		action.Method = "GET"
	}

	if action.Body != "" {
		req.Body(action.Body)
	}

	response, err := req.Do(action.Method, connection.URL)
	if err != nil {
		return nil, err
	}

	return response, nil
}
