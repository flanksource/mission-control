package views

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/duration"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

const valueFromMaxResults = 100

// Run executes the view queries and returns the rows with data
func Run(ctx context.Context, view *v1.View, request *requestOpt) (*api.ViewResult, error) {
	ctx = ctx.WithName("view")

	output := api.ViewResult{
		Namespace:          view.Namespace,
		Name:               view.Name,
		Icon:               view.Spec.Display.Icon,
		Title:              view.Spec.Display.Title,
		RequestFingerprint: request.Fingerprint(),
		Card:               view.Spec.Display.Card,
	}

	if len(request.variables) > 0 {
		st := ctx.NewStructTemplater(map[string]any{"var": request.variables}, "", nil)
		if err := st.Walk(&view.Spec.Queries); err != nil {
			return nil, fmt.Errorf("failed to template queries: %w", err)
		}
	}

	// Execute queries in parallel with a concurrency limit
	var queryResults []dataquery.QueryResultSet
	var mu sync.Mutex

	eg := errgroup.Group{}
	eg.SetLimit(10)

	for queryName, q := range view.Spec.Queries {
		eg.Go(func() error {
			queryStart := time.Now()
			results, err := pkgView.ExecuteQuery(ctx, q.Query)
			if err != nil {
				return fmt.Errorf("failed to execute view query '%s': %w", queryName, err)
			}
			queryDuration := time.Since(queryStart)
			ctx.Tracef("view=%s query=%s results=%d duration=%s", view.GetNamespacedName(), queryName, len(results), queryDuration)

			resultSet := dataquery.QueryResultSet{
				Results:    results,
				Name:       queryName,
				ColumnDefs: q.Columns,
			}

			if q.Configs != nil {
				resultSet.ColumnDefs = configQueryResultSchema
			} else if q.Changes != nil {
				resultSet.ColumnDefs = changeQueryResultSchema
			} else if q.Prometheus != nil {
				// When prometheus query returns no result, we a column def is necessary to
				// generate the table in sqlite3 database.
				if len(results) == 0 {
					if len(resultSet.ColumnDefs) == 0 {
						// It is assumed that the query only returns a single "value" column.
						resultSet.ColumnDefs = map[string]models.ColumnType{
							"value": models.ColumnTypeDecimal,
						}
					}
				} else {
					if len(resultSet.ColumnDefs) != 0 {
						if _, ok := resultSet.ColumnDefs["value"]; !ok {
							resultSet.ColumnDefs["value"] = models.ColumnTypeDecimal
						}
					}
				}
			}

			mu.Lock()
			queryResults = append(queryResults, resultSet)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// If there's no merge query and no panels,
	// there's no need to create an in-memory SQLite database.
	// The results from the dataqueries are directly mapped to the table columns.
	needsSQL := len(view.Spec.Panels) > 0 || view.Spec.Merge != nil

	var sqliteCtx context.Context
	if needsSQL {
		var err error
		var close func() error
		sqliteCtx, close, err = dataquery.DBFromResultsets(ctx, queryResults)
		if err != nil {
			return nil, fmt.Errorf("failed to create in-memory SQLite database: %w", err)
		}
		defer func() {
			if err := close(); err != nil {
				ctx.Errorf("failed to close in-memory SQLite database: %v", err)
			}
		}()
	}

	if len(view.Spec.Panels) > 0 {
		dataset := map[string]any{}
		for _, queryResult := range queryResults {
			dataset[queryResult.Name] = queryResult.Results
		}

		env := map[string]any{"dataset": dataset}
		for _, panel := range view.Spec.Panels {
			panelStart := time.Now()
			ctx.Logger.V(4).Infof("executing panel=%s type=%s", panel.Name, panel.Type)

			rows, err := dataquery.RunSQL(sqliteCtx, panel.Query)
			if err != nil {
				return nil, fmt.Errorf("failed to execute panel '%s': %w", panel.Name, err)
			}

			ctx.Logger.V(4).Infof("panel completed panel=%s rows=%d duration=%s", panel.Name, len(rows), time.Since(panelStart))

			switch panel.Type {
			case api.PanelTypeGauge:
				if panel.Gauge != nil && panel.Gauge.Max != "" {
					value, err := types.CelExpression(panel.Gauge.Max).Eval(env)
					if err != nil {
						return nil, fmt.Errorf("failed to evaluate gauge max expression '%s': %w", panel.Gauge.Max, err)
					}
					panel.Gauge.Max = value
				}
			case api.PanelTypeBargauge:
				if panel.Bargauge != nil && panel.Bargauge.Max != "" {
					value, err := types.CelExpression(panel.Bargauge.Max).Eval(env)
					if err != nil {
						return nil, fmt.Errorf("failed to evaluate bargauge max expression '%s': %w", panel.Bargauge.Max, err)
					}
					panel.Bargauge.Max = value
				}
			}

			output.Panels = append(output.Panels, api.PanelResult{
				PanelMeta: panel.PanelMeta,
				Rows:      rows,
			})
		}
	}

	if view.HasTable() {
		var mergedData []dataquery.QueryResultRow
		if lo.FromPtr(view.Spec.Merge) != "" {
			mergeStart := time.Now()
			var err error
			mergedData, err = dataquery.RunSQL(sqliteCtx, lo.FromPtr(view.Spec.Merge))
			if err != nil {
				return nil, ctx.Oops(dutyAPI.EINVALID).Wrapf(err, "failed to run sql query")
			}

			ctx.Logger.V(3).Infof("merge completed results=%d duration=%s", len(mergedData), time.Since(mergeStart))
		} else if len(queryResults) == 1 {
			mergedData = queryResults[0].Results
		} else {
			// NOTE: The view has multiple data sources defined however it's not clear which data source
			// must be used to populate the view table.
			return nil, ctx.Oops(dutyAPI.EINVALID).Errorf("multiple data sources defined but merge query is not specified")
		}

		var rows []pkgView.Row
		for _, row := range mergedData {
			row, err := applyMapping(ctx, row, view.Spec.Columns, view.Spec.Mapping)
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
func applyMapping(ctx context.Context, queryResultRow map[string]any, columnDefs []pkgView.ColumnDef, mapping map[string]types.CelExpression) (pkgView.Row, error) {
	var row pkgView.Row
	rowProperties := map[string]any{}

	for _, columnDef := range columnDefs {
		var rowValue any
		env := map[string]any{"row": queryResultRow} // We cannot directly pass result because of identifier collision with the reserved ones in cel.

		expr, ok := mapping[columnDef.Name]
		if !ok {
			// If mapping is not specified, look for the column name in the data row
			if value, ok := queryResultRow[columnDef.Name]; ok {
				rowValue = value
			} else {
				rowValue = nil
			}
		} else {
			value, err := expr.Eval(env)
			if err != nil {
				return nil, ctx.Oops().
					With("keys", collections.MapKeys(queryResultRow), ",").
					Errorf("failed to evaluate CEL expression for column %s: %w", columnDef.Name, err)
			}

			switch columnDef.Type {
			case pkgView.ColumnTypeDuration:
				v, err := duration.ParseDuration(value)
				if err != nil {
					return nil, fmt.Errorf("failed to parse as duration (%s): %w", value, err)
				}

				rowValue = time.Duration(v)

			case pkgView.ColumnTypeBytes:
				if value == "" {
					rowValue = int64(0)
				} else {
					v, err := strconv.ParseInt(value, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("failed to parse as int64 (%s): %w", value, err)
					}
					rowValue = int64(v)
				}

			case pkgView.ColumnTypeMillicore, pkgView.ColumnTypeGauge:
				if value == "" {
					rowValue = float64(0)
				} else {
					v, err := strconv.ParseFloat(value, 64)
					if err != nil {
						return nil, fmt.Errorf("failed to parse as float (%s): %w", value, err)
					}
					rowValue = float64(v)
				}

			default:
				rowValue = value
			}
		}

		row = append(row, rowValue)

		if attributes, err := getColumnAttributes(ctx, columnDef, queryResultRow); err != nil {
			return nil, fmt.Errorf("failed to evaluate attributes for column %s: %w", columnDef.Name, err)
		} else if len(attributes) > 0 {
			rowProperties[columnDef.Name] = attributes
		}
	}

	if len(rowProperties) > 0 {
		row = append(row, rowProperties)
	} else {
		row = append(row, nil)
	}

	return row, nil
}

// getColumnAttributes evaluates the column attributes for the given row.
func getColumnAttributes(ctx context.Context, columnDef pkgView.ColumnDef, row map[string]any) (map[string]any, error) {
	env := map[string]any{"row": row}
	attributes := map[string]any{}

	if columnDef.Type == pkgView.ColumnTypeConfigItem {
		idField := "id"
		if columnDef.ConfigItem != nil && columnDef.ConfigItem.IDField != "" {
			idField = columnDef.ConfigItem.IDField
		}

		if configID, ok := row[idField].(string); ok {
			config, err := query.GetCachedConfig(ctx, configID)
			if err != nil {
				return nil, ctx.Oops().Errorf("failed to get config(%s) for config_item column: %w", configID, err)
			} else if config != nil {
				attributes["config"] = map[string]string{
					"id":     config.ID.String(),
					"health": string(lo.FromPtr(config.Health)),
					"status": string(lo.FromPtr(config.Status)),
					"type":   string(lo.FromPtr(config.Type)),
					"class":  config.ConfigClass,
				}
			}
		}
	}

	if columnDef.Icon != nil {
		icon, err := types.CelExpression(*columnDef.Icon).Eval(env)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate icon expression '%s' for column %s: %w", *columnDef.Icon, columnDef.Name, err)
		}

		attributes["icon"] = icon
	}

	if columnDef.URL != nil {
		value, err := columnDef.URL.Eval(ctx, env)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate URL for column %s: %w", columnDef.Name, err)
		}

		attributes["url"] = value
	}

	if columnDef.Gauge != nil {
		if columnDef.Gauge.Max != "" {
			max, err := types.CelExpression(columnDef.Gauge.Max).Eval(env)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate gauge max expression '%s' for column %s: %w", columnDef.Gauge.Max, columnDef.Name, err)
			}

			attributes["max"] = max
		}

		if columnDef.Gauge.Min != "" {
			min, err := types.CelExpression(columnDef.Gauge.Min).Eval(env)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate gauge min expression '%s' for column %s: %w", columnDef.Gauge.Min, columnDef.Name, err)
			}

			attributes["min"] = min
		}
	}

	return attributes, nil
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
