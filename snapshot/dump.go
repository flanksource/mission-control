package snapshot

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
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

func dumpComponents(directory string, componentIDs []string) error {
	if len(componentIDs) == 0 {
		return nil
	}
	var jsonBlobs []map[string]any
	// Get entire row of components
	for _, componentID := range componentIDs {
		data, err := utils.HTTPGet(fmt.Sprintf(CanaryCheckerURL, componentID))
		if err != nil {
			return err
		}
		var jsonBlob []map[string]any
		err = json.Unmarshal([]byte(data), &jsonBlob)
		if err != nil {
			return err
		}

		jsonBlobs = append(jsonBlobs, jsonBlob...)
	}

	err := writeToJSONFile(directory, "components", jsonBlobs)
	if err != nil {
		return err
	}

	err = dumpTable("components", "id", "?", componentIDs, directory)
	if err != nil {
		return err
	}

	return nil
}

func dumpIncidents(directory string, incidentIDs []string) error {
	if len(incidentIDs) == 0 {
		return nil
	}

	err := dumpTable("incidents", "id", "?", incidentIDs, directory)
	if err != nil {
		return err
	}

	err = dumpTable("hypotheses", "incident_id", "?", incidentIDs, directory)
	if err != nil {
		return err
	}

	whereClause := `SELECT id FROM hypotheses WHERE incident_id IN (?)`
	err = dumpTable("evidences", "hypothesis_id", whereClause, incidentIDs, directory)
	if err != nil {
		return err
	}
	return nil
}

func dumpConfigs(directory string, configIDs []string) error {
	if len(configIDs) == 0 {
		return nil
	}
	// Get entire row of configItems
	err := dumpTable("config_items", "id", "?", configIDs, directory)
	if err != nil {
		return err
	}

	err = dumpTable("config_changes", "config_id", "?", configIDs, directory)
	if err != nil {
		return err
	}

	err = dumpTable("config_analysis", "config_id", "?", configIDs, directory)
	if err != nil {
		return err
	}

	return nil
}
