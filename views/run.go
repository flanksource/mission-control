package views

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	"github.com/flanksource/duty/models"
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
		results, err := pkgView.ExecuteQuery(ctx, q.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to execute config query '%s': %w", queryName, err)
		}

		resultSet := dataquery.QueryResultSet{
			Results:    results,
			Name:       queryName,
			ColumnDefs: q.Columns,
		}

		if q.Configs != nil {
			resultSet.ColumnDefs = configQueryResultSchema
		} else if q.Changes != nil {
			resultSet.ColumnDefs = changeQueryResultSchema
		}

		queryResults = append(queryResults, resultSet)
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

	for _, panel := range view.Spec.Panels {
		rows, err := dataquery.RunSQL(sqliteCtx, panel.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to execute panel '%s': %w", panel.Name, err)
		}

		output.Panels = append(output.Panels, api.PanelResult{
			PanelMeta: panel.PanelMeta,
			Rows:      rows,
		})
	}

	if len(view.Spec.Columns) != 0 {
		var mergedData []dataquery.QueryResultRow
		if lo.FromPtr(view.Spec.Merge) != "" {
			var err error
			mergedData, err = dataquery.RunSQL(sqliteCtx, lo.FromPtr(view.Spec.Merge))
			if err != nil {
				return nil, ctx.Oops(dutyAPI.EINVALID).Wrapf(err, "failed to run sql query")
			}
		} else if len(queryResults) == 1 {
			mergedData = queryResults[0].Results
		}

		var rows []pkgView.Row
		for _, result := range mergedData {
			row, err := applyMapping(result, view.Spec.Columns, view.Spec.Mapping)
			if err != nil {
				return nil, ctx.Oops(dutyAPI.EINVALID).Wrapf(err, "failed to apply view mapping")
			}

			rows = append(rows, row)
		}

		output.Rows = rows
		output.Columns = view.Spec.Columns

		output.Columns = append(output.Columns, pkgView.ColumnDef{
			Name: pkgView.ReservedColumnAttributes,
			Type: pkgView.ColumnTypeAttributes,
		})
	}

	return &output, nil
}

// applyMapping applies CEL expression mappings to data
func applyMapping(data map[string]any, columnDefs []pkgView.ColumnDef, mapping map[string]types.CelExpression) (pkgView.Row, error) {
	var row pkgView.Row
	rowProperties := map[string]any{}

	for _, columnDef := range columnDefs {
		var rowValue any

		expr, ok := mapping[columnDef.Name]
		if !ok {
			// If mapping is not specified, look for the column name in the data row
			if value, ok := data[columnDef.Name]; ok {
				rowValue = value
			} else {
				rowValue = nil
			}
		} else {
			env := map[string]any{"row": data} // We cannot directly pass result because of identifier collision with the reserved ones in cel.
			value, err := expr.Eval(env)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate CEL expression for column %s: %w", columnDef.Name, err)
			}

			switch columnDef.Type {
			case pkgView.ColumnTypeDuration:
				v, err := duration.ParseDuration(value)
				if err != nil {
					return nil, fmt.Errorf("failed to parse as duration (%s): %w", value, err)
				}

				rowValue = time.Duration(v)

			case pkgView.ColumnTypeGauge:
				rowValue = types.JSON(value)

			default:
				rowValue = value
			}
		}

		row = append(row, rowValue)

		if columnDef.HasAttributes() {
			properties := map[string]any{}

			env := map[string]any{"row": data}
			if columnDef.URL != nil {
				value, err := columnDef.URL.Eval(env)
				if err != nil {
					return nil, fmt.Errorf("failed to evaluate CEL expression for column %s: %w", columnDef.Name, err)
				}

				properties["url"] = value
			}

			if columnDef.Gauge != nil {
				if columnDef.Gauge.Max != "" {
					max, err := types.CelExpression(columnDef.Gauge.Max).Eval(env)
					if err != nil {
						return nil, fmt.Errorf("failed to evaluate gauge max expression '%s' for column %s: %w", columnDef.Gauge.Max, columnDef.Name, err)
					}

					properties["max"] = max
				}

				if columnDef.Gauge.Min != "" {
					min, err := types.CelExpression(columnDef.Gauge.Min).Eval(env)
					if err != nil {
						return nil, fmt.Errorf("failed to evaluate gauge min expression '%s' for column %s: %w", columnDef.Gauge.Min, columnDef.Name, err)
					}

					properties["min"] = min
				}
			}

			rowProperties[columnDef.Name] = properties
		}
	}

	if len(rowProperties) > 0 {
		row = append(row, rowProperties)
	} else {
		row = append(row, nil)
	}

	return row, nil
}

var configQueryResultSchema = map[string]models.ColumnType{
	"id":                models.ColumnTypeString,
	"agent_id":          models.ColumnTypeString,
	"icon":              models.ColumnTypeString,
	"scraper_id":        models.ColumnTypeString,
	"config_class":      models.ColumnTypeString,
	"status":            models.ColumnTypeString,
	"health":            models.ColumnTypeString,
	"ready":             models.ColumnTypeBoolean,
	"external_id":       models.ColumnTypeJSONB,
	"type":              models.ColumnTypeString,
	"cost_per_minute":   models.ColumnTypeDecimal,
	"cost_total_1d":     models.ColumnTypeDecimal,
	"cost_total_7d":     models.ColumnTypeDecimal,
	"cost_total_30d":    models.ColumnTypeDecimal,
	"name":              models.ColumnTypeString,
	"description":       models.ColumnTypeString,
	"config":            models.ColumnTypeJSONB,
	"source":            models.ColumnTypeString,
	"labels":            models.ColumnTypeJSONB,
	"tags":              models.ColumnTypeJSONB,
	"tags_values":       models.ColumnTypeJSONB,
	"properties":        models.ColumnTypeJSONB,
	"parent_id":         models.ColumnTypeString,
	"path":              models.ColumnTypeString,
	"is_pushed":         models.ColumnTypeBoolean,
	"created_by":        models.ColumnTypeString,
	"last_scraped_time": models.ColumnTypeString,
	"created_at":        models.ColumnTypeString,
	"updated_at":        models.ColumnTypeString,
	"deleted_at":        models.ColumnTypeString,
	"delete_reason":     models.ColumnTypeString,
	"inserted_at":       models.ColumnTypeString,
	"properties_values": models.ColumnTypeJSONB,
}

// Represents the catalog_changes view
var changeQueryResultSchema = map[string]models.ColumnType{
	"id":                  models.ColumnTypeString,
	"config_id":           models.ColumnTypeString,
	"name":                models.ColumnTypeString,
	"deleted_at":          models.ColumnTypeString,
	"type":                models.ColumnTypeString,
	"tags":                models.ColumnTypeJSONB,
	"config":              models.ColumnTypeJSONB,
	"external_created_by": models.ColumnTypeString,
	"created_at":          models.ColumnTypeString,
	"severity":            models.ColumnTypeString,
	"change_type":         models.ColumnTypeString,
	"source":              models.ColumnTypeString,
	"summary":             models.ColumnTypeString,
	"details":             models.ColumnTypeJSONB,
	"created_by":          models.ColumnTypeString,
	"count":               models.ColumnTypeInteger,
	"first_observed":      models.ColumnTypeString,
	"agent_id":            models.ColumnTypeString,
}
