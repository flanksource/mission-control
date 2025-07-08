package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func CreateViewTable(ctx context.Context, view *v1.View) error {
	tableName := view.TableName()
	if ctx.DB().Migrator().HasTable(tableName) {
		return nil
	}

	var columnDefs []string
	for _, col := range view.Spec.Columns {
		colDef := fmt.Sprintf("%s %s", col.Name, getPostgresType(col.Type))
		columnDefs = append(columnDefs, colDef)
	}

	columnDefs = append(columnDefs, "agent_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'::uuid")
	columnDefs = append(columnDefs, "is_pushed BOOLEAN DEFAULT FALSE")

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableName, strings.Join(columnDefs, ", "))
	return ctx.DB().Exec(sql).Error
}

func getPostgresType(colType api.ViewColumnType) string {
	switch colType {
	case api.ViewColumnTypeString:
		return "TEXT"
	case api.ViewColumnTypeNumber:
		return "NUMERIC"
	case api.ViewColumnTypeBoolean:
		return "BOOLEAN"
	case api.ViewColumnTypeDateTime:
		return "TIMESTAMP WITH TIME ZONE"
	case api.ViewColumnTypeDuration:
		return "BIGINT"
	case api.ViewColumnTypeHealth:
		return "TEXT"
	case api.ViewColumnTypeStatus:
		return "TEXT"
	case api.ViewColumnTypeGauge:
		return "JSONB"
	default:
		return "TEXT"
	}
}

func InsertPanelResults(ctx context.Context, viewID uuid.UUID, panels []api.PanelResult) error {
	results, err := json.Marshal(panels)
	if err != nil {
		return fmt.Errorf("failed to marshal panel results: %w", err)
	}

	record := models.ViewPanel{
		ViewID:  viewID,
		Results: results,
	}

	if err := ctx.DB().Save(&record).Error; err != nil {
		return fmt.Errorf("failed to save panel results: %w", err)
	}

	return nil
}

func InsertViewRows(ctx context.Context, tableName string, columns []api.ViewColumnDef, rows []api.ViewRow) error {
	if err := ctx.DB().Exec(fmt.Sprintf("TRUNCATE TABLE %s", tableName)).Error; err != nil {
		return fmt.Errorf("failed to clear existing data: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}

	var colNames []string
	for _, col := range columns {
		colNames = append(colNames, col.Name)
	}

	psql := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
	insertBuilder := psql.Insert(tableName).Columns(colNames...)
	for _, row := range rows {
		insertBuilder = insertBuilder.Values(row...)
	}

	sql, args, err := insertBuilder.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build insert query: %w", err)
	}

	return ctx.DB().Exec(sql, args...).Error
}

func ReadViewTable(ctx context.Context, columnDef api.ViewColumnDefList, tableName string) ([]api.ViewRow, error) {
	rows, err := ctx.DB().Select(columnDef.SelectColumns()).Table(tableName).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var viewRows []api.ViewRow
	for rows.Next() {
		viewRow := make(api.ViewRow, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range viewRow {
			valuePtrs[i] = &viewRow[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		viewRows = append(viewRows, viewRow)
	}

	return convertViewRecordsToNativeTypes(viewRows, columnDef), nil
}

// convertViewRecordsToNativeTypes converts view cell to native go types
func convertViewRecordsToNativeTypes(viewRows []api.ViewRow, columnDef []api.ViewColumnDef) []api.ViewRow {
	for _, viewRow := range viewRows {
		for i, colDef := range columnDef {
			if i >= len(viewRow) {
				continue
			}

			if viewRow[i] == nil {
				continue
			}

			switch colDef.Type {
			case api.ViewColumnTypeGauge:
				if raw, ok := viewRow[i].([]uint8); ok {
					viewRow[i] = json.RawMessage(raw)
				}

			case api.ViewColumnTypeDuration:
				switch v := viewRow[i].(type) {
				case int:
					viewRow[i] = time.Duration(v)
				case int32:
					viewRow[i] = time.Duration(v)
				case int64:
					viewRow[i] = time.Duration(v)
				case float64:
					viewRow[i] = time.Duration(int64(v))
				default:
					logger.Warnf("postProcessViewRows: unknown duration type: %T", v)
				}

			case api.ViewColumnTypeDateTime:
				switch v := viewRow[i].(type) {
				case time.Time:
					viewRow[i] = v
				case string:
					parsed, err := time.Parse(time.RFC3339, v)
					if err != nil {
						logger.Warnf("postProcessViewRows: failed to parse datetime: %v", err)
					}
					viewRow[i] = parsed
				default:
					logger.Warnf("postProcessViewRows: unknown datetime type: %T", v)
				}
			}
		}
	}

	return viewRows
}
