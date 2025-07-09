package views

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

// QueryResult represents all results from a single query
type QueryResult struct {
	Name string
	Rows []dataquery.QueryResultRow
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
		results, err := pkgView.ExecuteQuery(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("failed to execute config query '%s': %w", queryName, err)
		}

		queryResults = append(queryResults, QueryResult{
			Rows: results,
			Name: queryName,
		})
	}

	merge := lo.FromPtr(view.Spec.Merge)
	mergedData, err := mergeResults(queryResults, merge)
	if err != nil {
		return nil, fmt.Errorf("failed to merge results: %w", err)
	}

	var rows []pkgView.Row
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

// applyMapping applies CEL expression mappings to data
func applyMapping(data map[string]any, columnDefs []pkgView.ViewColumnDef, mapping map[string]types.CelExpression) (pkgView.Row, error) {
	var row pkgView.Row

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
		case pkgView.ColumnTypeDuration:
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
