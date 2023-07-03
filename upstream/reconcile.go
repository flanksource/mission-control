package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

var tablesToReconcile = []string{
	"components",
	"config_scrapers",
	"config_items",
	"canaries",
	"checks",
}

const pageSize = 5

func syncTableWithUpstream(ctx *api.Context, table string) error {
	logger.Debugf("Syncing %s with upstream", table)

	var next uuid.UUID
	for {
		paginateRequest := api.PushPaginateRequest{From: next, Table: table, Size: pageSize}

		upstreamStatus, err := fetchUpstreamStatus(ctx, api.UpstreamConf, paginateRequest)
		if err != nil {
			return fmt.Errorf("failed to fetch upstream status: %w", err)
		}

		if upstreamStatus.Total == 0 {
			break
		}

		current, err := db.GetIDsHash(ctx, table, next, pageSize)
		if err != nil {
			return fmt.Errorf("failed to fetch local id hash: %w", err)
		}

		next = upstreamStatus.Next
		if upstreamStatus.Hash == current.Hash {
			continue
		}

		resp, err := fetchUpstreamResourceIDs(ctx, api.UpstreamConf, paginateRequest)
		if err != nil {
			return fmt.Errorf("failed to fetch upstream resource ids: %w", err)
		}

		pushData, err := db.GetAllMissingResourceIDs(ctx, resp, paginateRequest)
		if err != nil {
			return fmt.Errorf("failed to fetch missing resource ids: %w", err)
		}

		pushData.AgentName = api.UpstreamConf.AgentName
		if err := Push(ctx, api.UpstreamConf, pushData); err != nil {
			return fmt.Errorf("failed to push missing resource ids: %w", err)
		}

		if upstreamStatus.Total < pageSize {
			break
		}
	}

	return nil
}

// SyncWithUpstream sends all the missing resources to the upstream.
func SyncWithUpstream(ctx *api.Context) error {
	jobHistory := models.NewJobHistory("SyncWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	for _, table := range tablesToReconcile {
		if err := syncTableWithUpstream(ctx, table); err != nil {
			jobHistory.AddError(err.Error())
			logger.Errorf("failed to sync table %s: %w", table, err)
		} else {
			jobHistory.IncrSuccess()
		}
	}

	return nil
}

// fetchUpstreamResourceIDs requests all the existing resource ids from the upstream
// that were sent by this agent.
func fetchUpstreamResourceIDs(ctx *api.Context, config api.UpstreamConfig, request api.PushPaginateRequest) ([]string, error) {
	endpoint, err := url.JoinPath(config.Host, "upstream", "pull", config.AgentName)
	if err != nil {
		return nil, fmt.Errorf("error creating url endpoint for host %s: %w", config.Host, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("http.NewRequest: %w", err)
	}

	query := req.URL.Query()
	query.Add("table", request.Table)
	query.Add("from", request.From.String())
	query.Add("size", fmt.Sprintf("%d", request.Size))
	req.URL.RawQuery = query.Encode()

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

	var response []string
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response, nil
}

func fetchUpstreamStatus(ctx context.Context, config api.UpstreamConfig, request api.PushPaginateRequest) (*api.PushResponse, error) {
	endpoint, err := url.JoinPath(config.Host, "upstream", "status", config.AgentName)
	if err != nil {
		return nil, fmt.Errorf("error creating url endpoint for host %s: %w", config.Host, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("http.NewRequest: %w", err)
	}

	query := req.URL.Query()
	query.Add("table", request.Table)
	query.Add("from", request.From.String())
	query.Add("size", fmt.Sprintf("%d", request.Size))
	req.URL.RawQuery = query.Encode()

	req.SetBasicAuth(config.Username, config.Password)
	httpClient := http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream server returned error status[%d]: %s", resp.StatusCode, string(respBody))
	}

	var response api.PushResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return &response, nil
}
