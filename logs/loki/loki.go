package loki

import (
	"encoding/json"
	"fmt"
	"io"
	netHTTP "net/http"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
)

func Fetch(ctx context.Context, baseURL string, auth *types.Authentication, request Request) (*LokiResponse, error) {
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL '%s': %w", baseURL, err)
	}

	apiURL := parsedBaseURL.JoinPath("/loki/api/v1/query_range")
	apiURL.RawQuery = request.Params().Encode()

	client := http.NewClient()
	if auth != nil {
		username, err := ctx.GetEnvValueFromCache(auth.Username, ctx.GetNamespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get username: %w", err)
		}
		password, err := ctx.GetEnvValueFromCache(auth.Password, ctx.GetNamespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get password: %w", err)
		}
		client.Auth(username, password)
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var lokiResp LokiResponse
	if err := json.Unmarshal(bodyBytes, &lokiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal loki response: %w", err)
	}

	return &lokiResp, nil
}
