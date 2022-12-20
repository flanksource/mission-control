package snapshot

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
)

type resource struct {
	componentIDs []string
	configIDs    []string
	incidentIDs  []string
}

func (src *resource) merge(dst resource) {
	src.componentIDs = append(src.componentIDs, dst.componentIDs...)
	src.configIDs = append(src.configIDs, dst.configIDs...)
	src.incidentIDs = append(src.incidentIDs, dst.incidentIDs...)
}

func (r resource) dump() error {
	err := dumpComponents(r.componentIDs)
	if err != nil {
		return err
	}

	err = dumpConfigs(r.configIDs)
	if err != nil {
		return err
	}

	err = dumpIncidents(r.incidentIDs)
	if err != nil {
		return err
	}
	return nil
}

func topologySnapshot(componentID string, related bool) {
	var resources resource
	componentIDs := []string{componentID}
	if related {
		// Get all related componentIDs
		// from: component_relationships
		componentIDs = append(componentIDs, "")
		relatedResources, err := fetchRelatedIDsForComponent(componentID)
		if err != nil {
			panic(err)
		}
		resources.merge(relatedResources)
	}
	resources.dump()

	// For all the componentIDs, get related configIDs
	// from config_component_relationships

	// For all components, get evidence-ids -> hypotheses -> incident ids

	// Then you have component ids, configsIDs and incidentIDs

	// Hit canary checker api to get json of this topology
	// related = true, check for component relationships and dump their json as well
}

func fetchRelatedIDsForComponent(componentID string) (resource, error) {
	var related resource

	// Fetch related componentIDs
	var componentIDs []string
	err := db.Gorm.Raw(`
        WITH RECURSIVE children AS (
            SELECT id as child, parent_id as parent
            FROM components
            WHERE parent_id is null
            UNION ALL
            SELECT m.id, COALESCE(c.parent,m.parent_id) 
            FROM components m
            JOIN children c ON m.parent_id = c.child
        )
        SELECT child FROM children WHERE parent = ?
    `, componentID).Scan(&componentIDs).Error
	if err != nil {
		return related, err
	}
	related.componentIDs = append(related.componentIDs, componentIDs...)

	componentIDs = []string{}
	err = db.Gorm.Raw(`
        SELECT relationship_id  FROM component_relationships WHERE component_id = @componentID
        UNION
        SELECT component_id FROM component_relationships WHERE relationship_id = @componentID 
    `, sql.Named("componentID", componentID)).Scan(&componentIDs).Error
	if err != nil {
		return related, err
	}
	related.componentIDs = append(related.componentIDs, componentIDs...)

	// Fetch related incidentIDs
	var incidentIDs []string
	err = db.Gorm.Raw(`
        SELECT id FROM incidents WHERE id IN (
            SELECT incident_id FROM hypotheses WHERE id IN (
                SELECT hypothesis_id FROM evidences WHERE component_id = ?
            )
        )`, componentID).Scan(&incidentIDs).Error

	related.incidentIDs = append(related.incidentIDs, incidentIDs...)

	// Fetch related configIDs
	var configIDs []string
	err = db.Gorm.Raw(`
        SELECT config_id FROM config_component_relationships WHERE component_id = ?
    `, componentID).Scan(&configIDs).Error

	related.configIDs = append(related.configIDs, configIDs...)

	return related, nil
}

func fetchRelatedIDsForConfig(configID string) (resource, error) {
	var related resource

	var configIDs []string
	err := db.Gorm.Raw(`
        WITH RECURSIVE children AS (
            SELECT id as child, parent_id as parent
            FROM config_items
            WHERE parent_id is null
            UNION ALL
            SELECT m.id, COALESCE(c.parent,m.parent_id) 
            FROM config_items m
            JOIN children c ON m.parent_id = c.child
        )
        SELECT child FROM children WHERE parent = ?
    `, configID).Scan(&configIDs).Error
	if err != nil {
		return related, err
	}
	related.configIDs = append(related.configIDs, configIDs...)

	// config_relationships
	configIDs = []string{}
	err = db.Gorm.Raw(`
        SELECT related_id  FROM config_relationships WHERE config_id = @configID
        UNION
        SELECT config_id FROM config_relationships WHERE related_id = @configID
    `, sql.Named("configID", configID)).Scan(&configIDs).Error
	if err != nil {
		return related, err
	}

	related.configIDs = append(related.configIDs, configIDs...)
	return related, nil
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
		Scan(&columns).Error

	if err != nil {
		panic(err)
	}
	return columns
}

func generateCSVDumpQuery(table, columns, idField, whereClause string) string {
	return fmt.Sprintf(`
        SELECT %s FROM %s WHERE %s IN (%s)
    `, columns, table, idField, whereClause)
}

func dumpComponents(componentIDs []string) error {
	if len(componentIDs) == 0 {
		return nil
	}
	var jsonBlobs []map[string]any
	//canaryCheckerURL := "http://canary-checker/api/topology/%s"
	canaryCheckerURL := "http://localhost:8090/api/topology/%s"
	// Get entire row of components
	for _, componentID := range componentIDs {
		data, err := utils.HTTPGet(fmt.Sprintf(canaryCheckerURL, componentID))
		if err != nil {
			panic(err)
		}
		var jsonBlob []map[string]any
		err = json.Unmarshal([]byte(data), &jsonBlob)
		if err != nil {
			panic(err)
		}

		jsonBlobs = append(jsonBlobs, jsonBlob...)
	}

	err := writeToJSONFile("/tmp", "components", jsonBlobs)
	if err != nil {
		panic(err)
	}

	err = dumpTable("components", "id", "?", componentIDs, "/tmp")
	if err != nil {
		panic(err)
	}

	return nil
}

func Test() {
	dumpIncidents([]string{"e190af93-7b22-430e-a29d-d54444614bfb", "5cd4df28-6711-4cc5-bbf8-5156fcd3e220"})
	dumpComponents([]string{})
}

func dumpIncidents(incidentIDs []string) error {
	if len(incidentIDs) == 0 {
		return nil
	}

	err := dumpTable("incidents", "id", "?", incidentIDs, "/tmp")
	if err != nil {
		panic(err)
	}

	err = dumpTable("hypotheses", "incident_id", "?", incidentIDs, "/tmp")
	if err != nil {
		panic(err)
	}

	whereClause := `SELECT id FROM hypotheses WHERE incident_id IN (?)`
	err = dumpTable("evidences", "hypothesis_id", whereClause, incidentIDs, "/tmp")
	if err != nil {
		panic(err)
	}
	return nil
}

func dumpTable(table string, idField, whereClause string, ids []string, csvDirectory string) error {
	var rows []map[string]any
	columnNames := getColumnNames(table)
	query := generateCSVDumpQuery(table, columnNames, idField, whereClause)
	err := db.Gorm.Raw(query, ids).Scan(&rows).Error
	if err != nil {
		return err
	}

	return writeToCSVFile(csvDirectory, table+".csv", columnNames, rows)
}

func dumpConfigs(configIDs []string) error {
	if len(configIDs) == 0 {
		return nil
	}
	// Get entire row of configItems
	err := dumpTable("config_items", "id", "?", configIDs, "/tmp")
	if err != nil {
		panic(err)
	}

	err = dumpTable("config_changes", "config_id", "?", configIDs, "/tmp")
	if err != nil {
		panic(err)
	}

	err = dumpTable("config_analysis", "config_id", "?", configIDs, "/tmp")
	if err != nil {
		panic(err)
	}

	return nil
}
