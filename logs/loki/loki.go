package loki

import (
	"encoding/json"
	"fmt"
	"io"
	netHTTP "net/http"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/logs"
)

func Fetch(ctx context.Context, conn connection.Loki, request Request) (*logs.LogResult, error) {
	if err := conn.Populate(ctx); err != nil {
		return nil, fmt.Errorf("failed to populate connection: %w", err)
	}

	parsedBaseURL, err := url.Parse(conn.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL '%s': %w", conn.URL, err)
	}
	apiURL := parsedBaseURL.JoinPath("/loki/api/v1/query_range")
	apiURL.RawQuery = request.Params().Encode()

	client := http.NewClient()

	if conn.Username != nil && conn.Password != nil {
		client.Auth(conn.Username.ValueStatic, conn.Password.ValueStatic)
	}

	resp, err := client.R(ctx).Get(apiURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response first cuz it may not always be JSON
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var lokiResp LokiResponse
	if err := json.Unmarshal(response, &lokiResp); err != nil {
		return nil, fmt.Errorf("%s", lo.Ellipsis(string(response), 256))
	}

	if resp.StatusCode != netHTTP.StatusOK {
		return nil, fmt.Errorf("loki request failed wth status %s: (error: %s, errorType: %s)", resp.Status, lokiResp.Error, lokiResp.ErrorType)
	}

	result := lokiResp.ToLogResult()
	return &result, nil
}
