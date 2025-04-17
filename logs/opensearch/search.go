package opensearch

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/flanksource/duty/context"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
	"github.com/samber/lo"
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

func (t *searcher) Search(ctx context.Context, q *Request) (*SearchResults, error) {
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

	var result SearchResults
	result.Results = lo.Map(r.Hits.Hits, func(hit SearchHit, _ int) map[string]any {
		return hit.Source
	})
	result.NextPage = r.Hits.NextPage(limit)
	return &result, nil
}
