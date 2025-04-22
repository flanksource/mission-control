package opensearch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/logs"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
)

type searcher struct {
	client *opensearch.Client
	config *Backend
}

func NewSearcher(ctx context.Context, backend Backend) (*searcher, error) {
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
		client: client,
		config: &backend,
	}, nil
}

func (t *searcher) Search(ctx context.Context, q *Request) (*logs.LogResult, error) {
	if q.Index == "" {
		return nil, ctx.Oops().Errorf("index is empty")
	}

	var limit = 50
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
		t.client.Search.WithSize(limit+1),
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
	for _, hit := range r.Hits.Hits {
		line := logs.LogLine{
			ID:     hit.ID,
			Labels: make(map[string]string),
		}

		for k, v := range hit.Source {
			switch k {
			case "@timestamp":
				line.FirstObserved = v.(string)
			case "message":
				line.Message = v.(string)
			case "log":
				if log, ok := v.(map[string]any); ok {
					if level, ok := log["level"].(string); ok {
						line.Severity = level
					}
				}
			default:
				mm, err := flatMap(k, v)
				if err != nil {
					return nil, ctx.Oops().Wrapf(err, "error mapping the field %s", k)
				}
				for k, v := range mm {
					line.Labels[k] = v
				}
			}
		}

		logResult.Logs = append(logResult.Logs, line)
	}

	return &logResult, nil
}

func flatMap(prefix string, v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}

	var m = make(map[string]string)
	switch vv := v.(type) {
	case map[string]any:
		for k, v := range vv {
			subMap, err := flatMap(k, v)
			if err != nil {
				return nil, err
			}

			for k, v := range subMap {
				key := fmt.Sprintf("%s.%s", prefix, k)
				if prefix == "" {
					key = k
				}
				m[key] = v
			}
		}

	default:
		if vvJSON, err := utils.Stringify(vv); err != nil {
			m[prefix] = vvJSON
		} else {
			m[prefix] = fmt.Sprintf("%v", vv)
		}
	}

	return m, nil
}
