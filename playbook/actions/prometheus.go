package actions

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
)

type Prometheus struct{}

type PrometheusResult struct {
	Query string                     `json:"query,omitempty"`
	Range *dataquery.PrometheusRange `json:"range,omitempty"`
	Rows  []dataquery.QueryResultRow `json:"rows,omitempty"`
	Count int                        `json:"count,omitempty"`
}

func (p *Prometheus) Run(ctx context.Context, query dataquery.PrometheusQuery) (*PrometheusResult, error) {
	rows, err := dataquery.ExecuteQuery(ctx, dataquery.Query{Prometheus: &query})
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to execute prometheus query")
	}

	return &PrometheusResult{
		Query: query.Query,
		Rows:  rows,
		Range: query.Range,
		Count: len(rows),
	}, nil
}
