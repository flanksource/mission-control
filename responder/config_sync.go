package responder

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

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
	ctx := api.NewContext(db.Gorm)
	var teams []api.Team
	if err := db.Gorm.Find(&teams).Error; err != nil {
		logger.Errorf("Error querying teams from database: %v", err)
		return
	}

	for _, team := range teams {
		if !team.HasResponder() {
			logger.Debugf("Skipping team %s since it does not have a responder", team.Name)
			continue
		}
		jobHistory := models.NewJobHistory("TeamResponderConfigSync", "team", team.ID.String())
		_ = db.PersistJobHistory(jobHistory.Start())

		defer func() {
			_ = db.PersistJobHistory(jobHistory.End())
		}()

		responder, err := GetResponder(ctx, team)
		if err != nil {
			logger.Errorf("Error getting responder: %v", err)
			jobHistory.AddError(err.Error()).End()
			continue
		}

		if configType, configName, config, err := responder.SyncConfig(ctx, team); err != nil {
			logger.Errorf("Error syncing config: %v", err)
			jobHistory.AddError(err.Error()).End()
			continue
		} else {
			if err := upsertConfig(configType, team.ID.String(), configName, config); err != nil {
				logger.Errorf("Error upserting config: %v", err)
				jobHistory.AddError(err.Error()).End()
				continue
			}
			jobHistory.IncrSuccess()

		}
	}
}
