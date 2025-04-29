package opensearch

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/flanksource/duty/context"
	opensearch "github.com/opensearch-project/opensearch-go/v2"

	"github.com/flanksource/incident-commander/logs"
)

type searcher struct {
	client        *opensearch.Client
	config        *Backend
	mappingConfig *logs.FieldMappingConfig
}

func NewSearcher(ctx context.Context, backend Backend, mappingConfig *logs.FieldMappingConfig) (*searcher, error) {
	cfg := opensearch.Config{
		Addresses: []string{backend.Address},
	}

	if backend.Username != nil {
		username, err := ctx.GetEnvValueFromCache(*backend.Username, ctx.GetNamespace())
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error getting the openSearch config")
		}
		cfg.Username = username
	}

	if backend.Password != nil {
		password, err := ctx.GetEnvValueFromCache(*backend.Password, ctx.GetNamespace())
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error getting the openSearch config")
		}
		cfg.Password = password
	}

	client, err := opensearch.NewClient(cfg)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "error creating the openSearch client")
	}

	pingResp, err := client.Ping()
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "error pinging the openSearch client")
	}

	if pingResp.StatusCode != 200 {
		return nil, ctx.Oops().Errorf("[opensearch] got ping response: %d", pingResp.StatusCode)
	}

	return &searcher{
		client:        client,
		config:        &backend,
		mappingConfig: mappingConfig,
	}, nil
}

func (t *searcher) Search(ctx context.Context, q *Request) (*logs.LogResult, error) {
	if q.Index == "" {
		return nil, ctx.Oops().Errorf("index is empty")
	}

	var limit = 500
	if q.Limit != "" {
		var err error
		limit, err = strconv.Atoi(q.Limit)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error converting limit to int")
		}
	}

	res, err := t.client.Search(
		t.client.Search.WithContext(ctx),
		t.client.Search.WithIndex(q.Index),
		t.client.Search.WithBody(strings.NewReader(q.Query)),
		t.client.Search.WithSize(limit),
		t.client.Search.WithErrorTrace(),
	)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "error searching")
	}
	defer res.Body.Close()

	var r SearchResponse
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, ctx.Oops().Wrapf(err, "error parsing the response body")
	}

	var logResult = logs.LogResult{}
	logResult.Logs = make([]logs.LogLine, 0, len(r.Hits.Hits))

	mappingConfig := DefaultFieldMappingConfig
	if t.mappingConfig != nil {
		mappingConfig = t.mappingConfig.WithDefaults(DefaultFieldMappingConfig)
	}

	for _, hit := range r.Hits.Hits {
		line := logs.LogLine{
			ID: hit.ID,
		}

		for k, v := range hit.Source {
			if err := logs.MapFieldToLogLine(k, v, &line, mappingConfig); err != nil {
				// Log or handle mapping error? For now, just log it.
				ctx.Warnf("Error mapping field %s for log %s: %v", k, line.ID, err)
			}
		}

		logResult.Logs = append(logResult.Logs, line)
	}

	return &logResult, nil
}

var DefaultFieldMappingConfig = logs.FieldMappingConfig{
	Message:   []string{"message"},
	Timestamp: []string{"@timestamp"},
	Severity:  []string{"log"},
}
