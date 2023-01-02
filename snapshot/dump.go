package snapshot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/antihax/optional"
	sdk "github.com/flanksource/canary-checker/sdk"

	"github.com/flanksource/incident-commander/api"
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

	var allComponents []sdk.Component
	for _, componentID := range componentIDs {
		components, _, err := api.Topology.TopologyQuery(context.Background(), &sdk.TopologyApiTopologyQueryOpts{
			Id: optional.NewString(componentID),
		})
		if err != nil {
			return err
		}
		allComponents = append(allComponents, components...)
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
		logs, err := components.GetLogsByComponent(componentID, ctx.LogStart)
		if err != nil {
			return err
		}

		if logs.Total == 0 {
			continue
		}

		jsonLogs, err := json.Marshal(logs.Results)
		if err != nil {
			return err
		}

		err = writeToLogFile(ctx.Directory, componentID+".log", jsonLogs)
		if err != nil {
			return err
		}
	}

	return nil
}
