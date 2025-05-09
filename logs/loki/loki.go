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

type lokiSearcher struct {
	conn          connection.Loki
	mappingConfig *logs.FieldMappingConfig
}

func NewSearcher(conn connection.Loki, mappingConfig *logs.FieldMappingConfig) *lokiSearcher {
	return &lokiSearcher{
		conn:          conn,
		mappingConfig: mappingConfig,
	}
}

func (t *lokiSearcher) Fetch(ctx context.Context, request Request) (*logs.LogResult, error) {
	if err := t.conn.Populate(ctx); err != nil {
		return nil, fmt.Errorf("failed to populate connection: %w", err)
	}

	parsedBaseURL, err := url.Parse(t.conn.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL '%s': %w", t.conn.URL, err)
	}
	apiURL := parsedBaseURL.JoinPath("/loki/api/v1/query_range")
	apiURL.RawQuery = request.Params().Encode()

	client := http.NewClient()

	if t.conn.Username != nil && t.conn.Password != nil {
		client.Auth(t.conn.Username.ValueStatic, t.conn.Password.ValueStatic)
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

	mappingConfig := DefaultFieldMappingConfig
	if t.mappingConfig != nil {
		mappingConfig = t.mappingConfig.WithDefaults(DefaultFieldMappingConfig)
	}

	result := lokiResp.ToLogResult(mappingConfig)

	return &result, nil
}

var DefaultFieldMappingConfig = logs.FieldMappingConfig{
	Severity: []string{"detected_level"},
	Host:     []string{"pod"},
}
