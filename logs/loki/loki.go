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

	if resp.StatusCode != netHTTP.StatusOK {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*10))
		if readErr != nil {
			return nil, fmt.Errorf("loki request failed with status %s (failed to read error response body: %v)", resp.Status, readErr)
		}

		return nil, fmt.Errorf("loki request failed with status %s: %s", resp.Status, string(bodyBytes))
	}

	var lokiResp LokiResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return nil, fmt.Errorf("failed to decode loki response: %w", err)
	}

	result := lokiResp.ToLogResult()
	return &result, nil
}
