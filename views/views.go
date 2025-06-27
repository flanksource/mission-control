package views

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

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

	configRows, err := executeConfigQueries(ctx, view.Spec.Columns, view.Spec.Queries.Configs)
	if err != nil {
		return nil, fmt.Errorf("failed to execute config queries: %w", err)
	}
	output.Rows = append(output.Rows, configRows...)

	changeRows, err := executeChangeQueries(ctx, view.Spec.Columns, view.Spec.Queries.Changes)
	if err != nil {
		return nil, fmt.Errorf("failed to execute change queries: %w", err)
	}
	output.Rows = append(output.Rows, changeRows...)

	output.Columns = view.Spec.Columns
	return &output, nil
}

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

// executeConfigQueries executes configuration-based queries
func executeConfigQueries(ctx context.Context, columnDefs []api.ViewColumnDef, queries []v1.ViewQuery) ([]api.ViewRow, error) {
	var rows []api.ViewRow

	for _, q := range queries {
		limit := q.Max
		if limit <= 0 {
			limit = -1 // No limit
		}

		configs, err := query.FindConfigsByResourceSelector(ctx, limit, q.Selector)
		if err != nil {
			return nil, fmt.Errorf("failed to find configs: %w", err)
		}

		// Process each config and apply mappings
		for _, config := range configs {
			row, err := applyMapping(map[string]any{
				"row": config.AsMap(),
			}, columnDefs, q.Mapping)
			if err != nil {
				return nil, fmt.Errorf("failed to apply mapping to config %s: %w", config.ID, err)
			}
			rows = append(rows, row)
		}
	}

	return rows, nil
}

// executeChangeQueries executes change-based queries using duty's FindConfigChangesByResourceSelector
func executeChangeQueries(ctx context.Context, columnDefs []api.ViewColumnDef, queries []v1.ViewQuery) ([]api.ViewRow, error) {
	var rows []api.ViewRow

	for _, q := range queries {
		limit := q.Max
		if limit <= 0 {
			limit = -1 // No limit
		}

		changes, err := query.FindConfigChangesByResourceSelector(ctx, limit, q.Selector)
		if err != nil {
			return nil, fmt.Errorf("failed to find changes: %w", err)
		}

		// Process each change and apply mappings
		for _, change := range changes {
			row, err := applyMapping(map[string]any{
				"row": change.AsMap(),
			}, columnDefs, q.Mapping)
			if err != nil {
				return nil, fmt.Errorf("failed to apply mapping to change %s: %w", change.ID, err)
			}
			rows = append(rows, row)
		}
	}

	return rows, nil
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

// ReadOrPopulateViewTable reads view data from the view's table.
// If the table does not exist, it will be created and the view will be populated.
func ReadOrPopulateViewTable(ctx context.Context, namespace, name string) (*api.ViewResult, error) {
	view, err := db.GetView(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get view: %w", err)
	}

	tableName := view.TableName()

	if !ctx.DB().Migrator().HasTable(tableName) {
		if err := db.CreateViewTable(ctx, view); err != nil {
			return nil, fmt.Errorf("failed to create view table: %w", err)
		}

		result, err := PopulateView(ctx, view)
		if err != nil {
			return nil, fmt.Errorf("failed to run view: %w", err)
		}

		return result, nil
	}

	rows, err := db.ReadViewTable(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to read view table: %w", err)
	}

	return &api.ViewResult{
		Columns: view.Spec.Columns,
		Rows:    rows,
	}, nil
}

// PopulateView runs the view queries and saves to the view table
func PopulateView(ctx context.Context, view *v1.View) (*api.ViewResult, error) {
	tableName := view.TableName()
	if !ctx.DB().Migrator().HasTable(tableName) {
		if err := db.CreateViewTable(ctx, view); err != nil {
			return nil, fmt.Errorf("failed to create view table: %w", err)
		}
	}

	// TODO: Must run panels and save them to the view table
	view.Spec.Panels = nil

	result, err := Run(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}

	return result, db.InsertViewRows(ctx, tableName, result.Columns, result.Rows)
}
