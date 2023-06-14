package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

// SyncWithUpstream sends all the missing resources to the upstream.
func SyncWithUpstream() error {
	ctx := context.Background()
	resp, err := fetchUpstreamResourceIDs(ctx, api.UpstreamConf)
	if err != nil {
		return fmt.Errorf("failed to fetch upstream resource ids: %w", err)
	}

	pushData, err := db.GetAllMissingResourceIDs(ctx, resp)
	if err != nil {
		return fmt.Errorf("failed to fetch missing resource ids: %w", err)
	}

	pushData.AgentName = api.UpstreamConf.AgentName
	if err := Push(ctx, api.UpstreamConf, pushData); err != nil {
		return fmt.Errorf("failed to push missing resource ids: %w", err)
	}

	return nil
}

// fetchUpstreamResourceIDs requests all the existing resource ids from the upstream
// that were sent by this agent.
func fetchUpstreamResourceIDs(ctx context.Context, config api.UpstreamConfig) (*api.PushedResourceIDs, error) {
	endpoint, err := url.JoinPath(config.Host, "upstream_check", config.AgentName)
	if err != nil {
		return nil, fmt.Errorf("error creating url endpoint for host %s: %w", config.Host, err)
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("http.NewRequest: %w", err)
	}

	req.SetBasicAuth(config.Username, config.Password)
	httpClient := http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream server returned error status[%d]: %s", resp.StatusCode, string(respBody))
	}

	var response api.PushedResourceIDs
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return &response, nil
}
