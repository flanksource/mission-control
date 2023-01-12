package responder

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/pkg/errors"

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
		jobHistory := models.NewJobHistory("TeamResponderConfigSync", "team", team.ID.String())
		_ = db.PersistJobHistory(jobHistory.Start())

		teamSpec, err := team.GetSpec()
		if err != nil {
			logger.Errorf("Error getting team spec: %v", err)
			jobHistory.AddError(err.Error()).End()
			continue
		}

		if teamSpec.ResponderClients.Jira != nil {
			if err := syncJiraConfig(team.ID.String(), teamSpec); err != nil {
				logger.Errorf("Error syncing Jira config: %v", err)
				jobHistory.AddError(err.Error())
			} else {
				jobHistory.IncrSuccess()
			}
		}

		if teamSpec.ResponderClients.MSPlanner != nil {
			if err := syncMSPlannerConfig(team.ID.String(), teamSpec); err != nil {
				logger.Errorf("Error syncing MSPlanner config: %v", err)
				jobHistory.AddError(err.Error())
			} else {
				jobHistory.IncrSuccess()
			}
		}

		_ = db.PersistJobHistory(jobHistory.End())
	}
}

func syncJiraConfig(teamID string, teamSpec api.TeamSpec) error {
	jiraClient, err := jiraClientFromTeamSpec(teamSpec)
	if err != nil {
		return errors.Wrap(err, "error instantiating Jira client")
	}

	jiraConfigJSON, err := jiraClient.GetConfigJSON()
	if err != nil {
		return errors.Wrap(err, "error generating config from Jira")
	}

	configName := teamSpec.ResponderClients.Jira.Values["project"]
	configType := responderNameConfigTypeMapping[JiraResponder]
	if err := upsertConfig(configType, teamID, configName, jiraConfigJSON); err != nil {
		return errors.Wrap(err, "error upserting Jira config into database")
	}
	return nil
}

func syncMSPlannerConfig(teamID string, teamSpec api.TeamSpec) error {
	msPlannerClient, err := msPlannerClientFromTeamSpec(teamSpec)
	if err != nil {
		return errors.Wrap(err, "error instantiating MSPlanner client")
	}

	msPlannerConfigJSON, err := msPlannerClient.GetConfigJSON()
	if err != nil {
		return errors.Wrap(err, "error generating config from MSPlanner")
	}

	configName := teamSpec.ResponderClients.MSPlanner.Values["plan"]
	configType := responderNameConfigTypeMapping[MSPlannerResponder]
	if err = upsertConfig(configType, teamID, configName, msPlannerConfigJSON); err != nil {
		return errors.Wrap(err, "error upserting MSPlanner config into database")
	}
	return nil
}
