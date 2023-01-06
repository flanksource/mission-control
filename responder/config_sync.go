package responder

import (
	"github.com/flanksource/commons/logger"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

var responderNameConfigTypeMapping = map[string]string{
	JiraResponder:      "Jira",
	MSPlannerResponder: "MSPlanner",
}

func upsertConfig(configType, externalID, name, config string) error {

	dbInsertConfigQuery := `INSERT INTO config_items (config_type, name, external_id, config) VALUES (?, ?, ARRAY[?], ?)`
	dbUpdateConfigQuery := `UPDATE config_items SET config = ? WHERE external_id = ARRAY[?] AND config_type = ?`

	tx := db.Gorm.Exec(dbUpdateConfigQuery, config, externalID, configType)
	if tx.Error != nil {
		logger.Errorf("Error updating config in database: %v", tx.Error)
		return tx.Error
	}

	if tx.RowsAffected == 0 {
		if err := db.Gorm.Exec(dbInsertConfigQuery, configType, name, externalID, config).Error; err != nil {
			logger.Errorf("Error inserting config into database: %v", err)
			return tx.Error
		}
	}

	return nil
}

func SyncConfig() {
	logger.Debugf("Syncing responder config")
	var teams []api.Team
	if err := db.Gorm.Find(&teams).Error; err != nil {
		logger.Errorf("Error querying teams from database: %v", err)
		return
	}

	for _, team := range teams {
		teamSpec, err := team.GetSpec()
		if err != nil {
			logger.Errorf("Error getting team spec: %v", err)
			continue
		}

		if teamSpec.ResponderClients.Jira != nil {
			jiraClient, err := jiraClientFromTeamSpec(teamSpec)
			if err != nil {
				logger.Errorf("Error instantiating Jira client: %v", err)
				continue
			}

			jiraConfigJSON, err := jiraClient.GetConfigJSON()
			if err != nil {
				logger.Errorf("Error generating config from Jira: %v", err)
				continue
			}

			configName := teamSpec.ResponderClients.Jira.Values["project"]
			configType := responderNameConfigTypeMapping[JiraResponder]
			if err = upsertConfig(configType, team.ID.String(), configName, jiraConfigJSON); err != nil {
				logger.Errorf("Error upserting Jira config into database: %v", err)
				continue
			}
		}

		if teamSpec.ResponderClients.MSPlanner != nil {
			msPlannerClient, err := msPlannerClientFromTeamSpec(teamSpec)
			if err != nil {
				logger.Errorf("Error instantiating MSPlanner client: %v", err)
				continue
			}

			msPlannerConfigJSON, err := msPlannerClient.GetConfigJSON()
			if err != nil {
				logger.Errorf("Error generating config from MSPlanner: %v", err)
				continue
			}

			configName := teamSpec.ResponderClients.MSPlanner.Values["plan"]
			configType := responderNameConfigTypeMapping[MSPlannerResponder]
			if err = upsertConfig(configType, team.ID.String(), configName, msPlannerConfigJSON); err != nil {
				logger.Errorf("Error upserting MSPlanner config into database: %v", err)
				continue
			}
		}
	}
}
