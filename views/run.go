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

// Run executes the view queries and returns the rows with data
func Run(ctx context.Context, view *v1.View) (*api.ViewResult, error) {
	var output api.ViewResult

	var queryResults []dataquery.QueryResultSet
	for queryName, q := range view.Spec.Queries {
		results, err := pkgView.ExecuteQuery(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("failed to execute config query '%s': %w", queryName, err)
		}

		queryResults = append(queryResults, dataquery.QueryResultSet{
			Results: results,
			Name:    queryName,
		})
	}

	sqliteCtx, close, err := dataquery.DBFromResultsets(ctx, queryResults)
	if err != nil {
		return nil, fmt.Errorf("failed to create in-memory SQLite database: %w", err)
	}
	defer func() {
		if err := close(); err != nil {
			ctx.Errorf("failed to close in-memory SQLite database: %v", err)
		}
	}()

	for _, summary := range view.Spec.Panels {
		summaryRows, err := dataquery.RunSQL(sqliteCtx, summary.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to execute panel '%s': %w", summary.Name, err)
		}

		output.Panels = append(output.Panels, api.PanelResult{
			PanelMeta: summary.PanelMeta,
			Rows:      summaryRows,
		})
	}

	if len(view.Spec.Columns) != 0 {
		var mergedData []dataquery.QueryResultRow
		if lo.FromPtr(view.Spec.Merge) != "" {
			var err error
			mergedData, err = dataquery.RunSQL(sqliteCtx, lo.FromPtr(view.Spec.Merge))
			if err != nil {
				return nil, fmt.Errorf("failed to merge results: %w", err)
			}
		} else if len(queryResults) == 1 {
			mergedData = queryResults[0].Results
		}

		var rows []pkgView.Row
		for _, result := range mergedData {
			env := map[string]any{"row": result} // We cannot directly pass result because of identifier collision with the reserved ones in cel.
			row, err := applyMapping(env, view.Spec.Columns, view.Spec.Mapping)
			if err != nil {
				return nil, fmt.Errorf("failed to apply view mapping: %w", err)
			}

			rows = append(rows, row)
		}

		output.Rows = rows
		output.Columns = view.Spec.Columns
	}

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
