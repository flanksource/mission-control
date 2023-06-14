package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
)

func ReconcileJob() {
	if err := reconcileJob(); err != nil {
		logger.Errorf("error reconciling job: %v", err)
	}
}

func reconcileJob() error {
	ctx := context.Background()
	resp, err := requestIDs(ctx, api.UpstreamConf)
	if err != nil {
		return err
	}

	logger.Infof("%v", resp)

	// TODO: Find all the missing ids and then push it
	var msg api.PushData
	if err := Push(ctx, api.UpstreamConf, &msg); err != nil {
		return err
	}

	return nil
}

func requestIDs(ctx context.Context, config api.UpstreamConfig) (*api.IDsResponse, error) {
	endpoint, err := url.JoinPath(config.Host, "upstream_push")
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

	var response api.IDsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return &response, nil
}
