package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/duty"
	"github.com/flanksource/incident-commander/components"
	"github.com/flanksource/incident-commander/db"
)

func getColumnNames(table string) (string, error) {
	var columns string
	err := db.Gorm.Raw(`SELECT string_agg(column_name, ',') from information_schema.columns where table_name = ?`, table).
		Scan(&columns).Error

	if err != nil {
		return "", err
	}
	return columns, nil
}

func generateCSVDumpQuery(table, columns, idField, whereClause string) string {
	return fmt.Sprintf(`
        SELECT %s FROM %s WHERE %s IN (%s)
    `, columns, table, idField, whereClause)
}

func dumpTable(table string, idField, whereClause string, ids []string, csvDirectory string) error {
	var rows []map[string]any
	columnNames, err := getColumnNames(table)
	if err != nil {
		return err
	}
	query := generateCSVDumpQuery(table, columnNames, idField, whereClause)
	err = db.Gorm.Raw(query, ids).Scan(&rows).Error
	if err != nil {
		return err
	}

	return writeToCSVFile(csvDirectory, table+".csv", columnNames, rows)
}

func dumpComponents(ctx SnapshotContext, componentIDs []string) error {
	if len(componentIDs) == 0 {
		return nil
	}

	var allComponents models.Components
	for _, componentID := range componentIDs {
		response, err := duty.QueryTopology(context.Background(), db.Pool, duty.TopologyOptions{
			ID: componentID,
		})
		if err != nil {
			logger.Errorf("Error querying topology: %v", err)
			return err
		}

		allComponents = append(allComponents, response.Components...)
	}

	jsonBlob, err := json.Marshal(allComponents)
	if err != nil {
		return err
	}

	err = writeToJSONFile(ctx.Directory, "components.json", jsonBlob)
	if err != nil {
		return err
	}

	err = dumpTable("components", "id", "?", componentIDs, ctx.Directory)
	if err != nil {
		return err
	}

	err = dumpLogs(ctx, componentIDs)
	if err != nil {
		return err
	}

	return nil
}

func dumpIncidents(ctx SnapshotContext, incidentIDs []string) error {
	if len(incidentIDs) == 0 {
		return nil
	}

	err := dumpTable("incidents", "id", "?", incidentIDs, ctx.Directory)
	if err != nil {
		return err
	}

	err = dumpTable("hypotheses", "incident_id", "?", incidentIDs, ctx.Directory)
	if err != nil {
		return err
	}

	whereClause := `SELECT id FROM hypotheses WHERE incident_id IN (?)`
	err = dumpTable("evidences", "hypothesis_id", whereClause, incidentIDs, ctx.Directory)
	if err != nil {
		return err
	}
	return nil
}

func dumpConfigs(ctx SnapshotContext, configIDs []string) error {
	if len(configIDs) == 0 {
		return nil
	}

	err := dumpTable("config_items", "id", "?", configIDs, ctx.Directory)
	if err != nil {
		return err
	}

	err = dumpTable("config_changes", "config_id", "?", configIDs, ctx.Directory)
	if err != nil {
		return err
	}

	err = dumpTable("config_analysis", "config_id", "?", configIDs, ctx.Directory)
	if err != nil {
		return err
	}

	return nil
}

func dumpLogs(ctx SnapshotContext, componentIDs []string) error {
	for _, componentID := range componentIDs {
		logResult, err := components.GetLogsByComponent(componentID, ctx.LogStart, ctx.LogEnd)
		if err != nil {
			return err
		}

		if len(logResult.Logs) == 0 {
			continue
		}

		var logDump []byte
		switch ctx.LogFormat {
		case LogFormatLog:
			var rawLogs []string
			for _, logline := range logResult.Logs {
				rawLogs = append(rawLogs, fmt.Sprintf("[%s] %s {%s}", logline.Timestamp, logline.Message, logline.Labels))
			}
			logDump = []byte(strings.Join(rawLogs, "\n"))
		case LogFormatJSON:
			logDump, err = json.Marshal(logResult)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid logFormat: %s", ctx.LogFormat)
		}

		logFilename := fmt.Sprintf("logs-%s-%s-%s.%s", logResult.Type, logResult.Name, hash.Sha256Hex(componentID), ctx.LogFormat)
		err = writeToLogFile(ctx.Directory, logFilename, logDump)
		if err != nil {
			return err
		}
	}

	return nil
}
