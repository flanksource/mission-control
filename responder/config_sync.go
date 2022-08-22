package responder

import (
	"encoding/json"
	"time"

	"github.com/flanksource/commons/logger"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func StartConfigSync() {
	for {
		logger.Infof("Syncing configuration")
		syncConfig()

		time.Sleep(10 * time.Minute)
	}
}

func syncConfig() {
	var teams []api.Team
	if err := db.Gorm.Find(&teams).Error; err != nil {
		logger.Errorf("Error querying teams from database: %v", err)
		return
	}

	dbInsertConfigQuery := `INSERT INTO config_item (config_type, name, external_id, config) VALUES (?, ?, ?, ?)`

	for _, team := range teams {
		teamSpecJson, err := team.Spec.MarshalJSON()
		if err != nil {
			logger.Errorf("Error marshalling team spec into json: %v", err)
			continue
		}
		var teamSpec api.TeamSpec
		if err = json.Unmarshal(teamSpecJson, &teamSpec); err != nil {
			logger.Errorf("Error unmarshalling team spec into struct: %v", err)
			continue
		}

		if teamSpec.ResponderClients.Jira != (api.JiraClient{}) {
			jiraClient, err := jiraClientFromTeamSpec(team.Spec)
			if err != nil {
				logger.Errorf("Error instantiating Jira client: %v", err)
				continue
			}

			jiraConfigJSON, err := jiraClient.GetConfigJSON()
			if err != nil {
				logger.Errorf("Error generating config from Jira: %v", err)
				continue
			}

			if err = db.Gorm.Exec(dbInsertConfigQuery, JiraResponder, "Jira", team.ID, jiraConfigJSON).Error; err != nil {
				logger.Errorf("Error inserting Jira config into database: %v", err)
				continue
			}

		}

		if teamSpec.ResponderClients.MSPlanner != (api.MSPlannerClient{}) {
			msPlannerClient, err := msPlannerClientFromTeamSpec(team.Spec)
			if err != nil {
				logger.Errorf("Error instantiating MSPlanner client: %v", err)
				continue
			}

			msPlannerConfigJSON, err := msPlannerClient.GetConfigJSON()
			if err != nil {
				logger.Errorf("Error generating config from MSPlanner: %v", err)
				continue
			}

			if err = db.Gorm.Exec(dbInsertConfigQuery, MSPlannerResponder, "MSPlanner", team.ID, msPlannerConfigJSON).Error; err != nil {
				logger.Errorf("Error inserting MSPlanner config into database: %v", err)
				continue
			}
		}

	}
}
