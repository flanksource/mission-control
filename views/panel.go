package views

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
)

func executePanel(ctx context.Context, q api.PanelDef) ([]types.AggregateRow, error) {
	table := "config_items"
	if q.Source == "changes" {
		table = "catalog_changes"
	}

	result, err := query.Aggregate(ctx, table, q.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate: %w", err)
	}

	return result, nil
}
