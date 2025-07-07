package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

// QueryResult represents all results from a single query
type QueryResult struct {
	Name       string
	PrimaryKey []string
	Rows       []QueryResultRow
}

type QueryResultRow map[string]any

func (r QueryResultRow) PK(pk []string) string {
	var pkValues []string
	for _, key := range pk {
		pkValues = append(pkValues, fmt.Sprintf("%v", r[key]))
	}

	return strings.Join(pkValues, "-")
}

// Run executes the view queries and returns the rows with data
func Run(ctx context.Context, view *v1.View) (*api.ViewResult, error) {
	output := api.ViewResult{}

	for _, summary := range view.Spec.Panels {
		summaryRows, err := executePanel(ctx, summary)
		if err != nil {
			return nil, fmt.Errorf("failed to execute panel '%s': %w", summary.Name, err)
		}

		output.Panels = append(output.Panels, api.PanelResult{
			PanelMeta: summary.PanelMeta,
			Rows:      summaryRows,
		})
	}

	var queryResults []QueryResult
	for queryName, q := range view.Spec.Queries {
		results, err := executeQuery(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("failed to execute config query '%s': %w", queryName, err)
		}

		queryResults = append(queryResults, QueryResult{
			Rows:       results,
			Name:       queryName,
			PrimaryKey: q.PrimaryKey,
		})
	}

	var mergedData []QueryResultRow
	if view.Spec.Merge != nil {
		strategy := view.Spec.Merge.Strategy
		if strategy == "" {
			strategy = v1.ViewMergeStrategyUnion
		}

		order := view.Spec.Merge.Order
		if len(order) == 0 {
			for queryName := range view.Spec.Queries {
				order = append(order, queryName)
			}
		}

		mergedData = mergeResults(queryResults, order, strategy)
	} else {
		mergedData = mergeResults(queryResults, nil, v1.ViewMergeStrategyUnion)
	}

	var rows []api.ViewRow
	for _, result := range mergedData {
		row, err := applyMapping(result, view.Spec.Columns, view.Spec.Mapping)
		if err != nil {
			return nil, fmt.Errorf("failed to apply view mapping: %w", err)
		}

		rows = append(rows, row)
	}

	output.Rows = rows
	output.Columns = view.Spec.Columns
	return &output, nil
}

// executeQuery executes a single query and returns results with query name
func executeQuery(ctx context.Context, q v1.ViewQuery) ([]QueryResultRow, error) {
	var results []QueryResultRow

	if q.Configs != nil && !q.Configs.IsEmpty() {
		configs, err := query.FindConfigsByResourceSelector(ctx, -1, *q.Configs)
		if err != nil {
			return nil, fmt.Errorf("failed to find configs: %w", err)
		}

		for _, config := range configs {
			results = append(results, config.AsMap())
		}
	} else if q.Changes != nil && !q.Changes.IsEmpty() {
		changes, err := query.FindConfigChangesByResourceSelector(ctx, -1, *q.Changes)
		if err != nil {
			return nil, fmt.Errorf("failed to find changes: %w", err)
		}

		for _, change := range changes {
			results = append(results, change.AsMap())
		}
	} else {
		return nil, fmt.Errorf("view query has not datasource specified")
	}

	return results, nil
}

// applyMapping applies CEL expression mappings to data
func applyMapping(data map[string]any, columnDefs []api.ViewColumnDef, mapping map[string]types.CelExpression) (api.ViewRow, error) {
	var row api.ViewRow

	for _, columnDef := range columnDefs {
		expr, ok := mapping[columnDef.Name]
		if !ok {
			row = append(row, nil)
			continue
		}

		value, err := expr.Eval(data)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate CEL expression for column %s: %w", columnDef.Name, err)
		}

		switch columnDef.Type {
		case api.ViewColumnTypeDuration:
			v, err := duration.ParseDuration(value)
			if err != nil {
				return nil, fmt.Errorf("failed to parse as duration (%s): %w", value, err)
			}

			row = append(row, time.Duration(v))

		default:
			row = append(row, value)
		}
	}

	return row, nil
}
