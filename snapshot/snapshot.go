package snapshot

import (
	"fmt"

	"github.com/flanksource/incident-commander/db"
)

func topologySnapshot(componentID string, related bool) {
	componentIDs := []string{componentID}
	if related {
		// Get all related componentIDs
		// from: component_relationships
		componentIDs = append(componentIDs, "")
	}

	// For all the componentIDs, get related configIDs
	// from config_component_relationships

	// For all components, get evidence-ids -> hypotheses -> incident ids

	// Then you have component ids, configsIDs and incidentIDs

	// Hit canary checker api to get json of this topology
	// related = true, check for component relationships and dump their json as well
}

func IncidentSnapshot(incidentID string) {
	// Get incident row
	// Get all hypotheses for that incident
	// Get all evidence for that incident
}

func ConfigSnapshot(configID string) {
	// Get config dump
	// If related = true, get related config_ids as well
	// Take dump of all the config changes
	// Take dump of all the config analysis
}

func getColumnNames(table string) string {
	var columns string
	err := db.Gorm.Raw(`SELECT string_agg(column_name, ',') from information_schema.columns where table_name = ?`, table).
		Scan(columns).Error

	if err != nil {
		panic(err)
	}
	return columns
}

func generateCSVDumpQuery(table, idField, whereClause string) string {
	columns := getColumnNames(table)
	return fmt.Sprintf(`
        SELECT CONCAT_WS(',', %s) FROM %s WHERE %s IN (%s)
    `, columns, table, idField, whereClause)
}

func dumpComponents(componentIDs []string) {
	// Get entire row of components
	// Get all their trees json by hitting API
}

func dumpIncidents(incidentIDs []string) {
	// Get entire row of incidents
	// Get entire row of all the hypotheses
	// Get entire row of all the evidences
	var incidents []string
	query := generateCSVDumpQuery("incidents", "id", "?")
	err := db.Gorm.Raw(query, incidentIDs).Scan(&incidents).Error
	if err != nil {
		panic(err)
	}

	var hypotheses []string
	query = generateCSVDumpQuery("hypotheses", "incident_id", "?")
	err = db.Gorm.Raw(query, incidentIDs).Scan(&hypotheses).Error

	var evidences []string
	inClause := `SELECT id FROM FROM hypotheses WHERE incident_id IN (?)`
	query = generateCSVDumpQuery("evidences", "hypothesis_id", inClause)
	err = db.Gorm.Raw(query, incidentIDs).Scan(&evidences).Error
}

func dumpConfigs(configIDs []string) {
	// Get entire row of configItems
	var configItems []string
	query := generateCSVDumpQuery("config_items", "id", "?")
	err := db.Gorm.Raw(query, configIDs).Scan(&configItems).Error
	if err != nil {
		panic(err)
	}

	var configChanges []string
	query = generateCSVDumpQuery("config_changes", "config_id", "?")
	err = db.Gorm.Raw(query, configIDs).Scan(&configChanges).Error
	if err != nil {
		panic(err)
	}

	var configAnalysis []string
	query = generateCSVDumpQuery("config_analysis", "config_id", "?")
	err = db.Gorm.Raw(query, configIDs).Scan(&configAnalysis).Error
	if err != nil {
		panic(err)
	}

	// Get entire row of config changes
	// Get entire row of config analysis
}
