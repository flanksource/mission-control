package actions

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

type Prometheus struct{}

type PrometheusResult struct {
	Query string                     `json:"query,omitempty"`
	Range *v1.PrometheusActionRange  `json:"range,omitempty"`
	Rows  []dataquery.QueryResultRow `json:"rows,omitempty"`
	Count int                        `json:"count,omitempty"`
}

func (p *Prometheus) Run(ctx context.Context, action v1.PrometheusAction) (*PrometheusResult, error) {
	query := action.ToPrometheusQuery()

	rows, err := dataquery.ExecuteQuery(ctx, dataquery.Query{Prometheus: &query})
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to execute prometheus query")
	}

	return &PrometheusResult{
		Query: action.Query,
		Rows:  rows,
		Range: action.Range,
		Count: len(rows),
	}, nil
}
