package views

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

// QueryResult represents all results from a single query
type QueryResult struct {
	Name string
	Rows []QueryResultRow
}

type QueryResultRow map[string]any

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
			Rows: results,
			Name: queryName,
		})
	}

	merge := lo.If(view.Spec.Merge != nil, *view.Spec.Merge).Else(v1.ViewMergeSpec{})
	mergedData, err := mergeResults(queryResults, merge)
	if err != nil {
		return nil, fmt.Errorf("failed to merge results: %w", err)
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
	} else if q.Prometheus != nil {
		prometheusResults, err := executePrometheusQuery(ctx, *q.Prometheus)
		if err != nil {
			return nil, fmt.Errorf("failed to execute prometheus query: %w", err)
		}

		results = prometheusResults
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
