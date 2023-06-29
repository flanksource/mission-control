package responder

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

// A shared config class for all responder configs.
const configClass = "Responder"

func upsertConfig(configType, externalID, name, config string) error {
	dbUpdateConfigQuery := `UPDATE config_items SET config = ? WHERE external_id = ARRAY[?] AND type = ? AND config_class = ?`
	tx := db.Gorm.Exec(dbUpdateConfigQuery, config, externalID, configType, configClass)
	if tx.Error != nil {
		logger.Errorf("Error updating config into database: %v", tx.Error)
		return tx.Error
	}

	if tx.RowsAffected == 0 {
		dbInsertConfigQuery := `INSERT INTO config_items (config_class, type, name, external_id, config) VALUES (?, ?, ?, ARRAY[?], ?)`
		if err := db.Gorm.Exec(dbInsertConfigQuery, configClass, configType, name, externalID, config).Error; err != nil {
			logger.Errorf("Error inserting config into database: %v", err)
			return tx.Error
		}
	}

	return nil
}

func SyncConfig() {
	logger.Debugf("Syncing responder config")
	ctx := api.NewContext(db.Gorm, nil)
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
