package responder

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

// A shared config class for all responder configs.
const configClass = "Responder"

func upsertConfig(ctx context.Context, configType, externalID, name, config string) error {
	dbUpdateConfigQuery := `UPDATE config_items SET config = ? WHERE external_id = ARRAY[?] AND type = ? AND config_class = ?`
	tx := ctx.DB().Exec(dbUpdateConfigQuery, config, externalID, configType, configClass)
	if tx.Error != nil {
		logger.Errorf("Error updating config into database: %v", tx.Error)
		return tx.Error
	}

	if tx.RowsAffected == 0 {
		dbInsertConfigQuery := `INSERT INTO config_items (config_class, type, name, external_id, config) VALUES (?, ?, ?, ARRAY[?], ?)`
		if err := ctx.DB().Exec(dbInsertConfigQuery, configClass, configType, name, externalID, config).Error; err != nil {
			logger.Errorf("Error inserting config into database: %v", err)
			return tx.Error
		}
	}

	return nil
}

func SyncConfig(ctx job.JobRuntime) error {
	logger.Debugf("Syncing responder config")

	var teams []api.Team
	if err := ctx.DB().Find(&teams).Error; err != nil {
		logger.Errorf("Error querying teams from database: %v", err)
		return err
	}

	for _, team := range teams {
		if !team.HasResponder() {
			logger.Debugf("Skipping team %s since it does not have a responder", team.Name)
			continue
		}
		jobHistory := models.NewJobHistory("TeamResponderConfigSync", "team", team.ID.String())
		_ = db.PersistJobHistory(ctx.Context, jobHistory.Start())

		defer func() {
			_ = db.PersistJobHistory(ctx.Context, jobHistory.End())
		}()

		responder, err := GetResponder(ctx.Context, team)
		if err != nil {
			logger.Errorf("Error getting responder: %v", err)
			jobHistory.AddError(err.Error()).End()
			continue
		}

		if configType, configName, config, err := responder.SyncConfig(ctx.Context, team); err != nil {
			logger.Errorf("Error syncing config: %v", err)
			jobHistory.AddError(err.Error()).End()
			continue
		} else {
			if err := upsertConfig(ctx.Context, configType, team.ID.String(), configName, config); err != nil {
				logger.Errorf("Error upserting config: %v", err)
				jobHistory.AddError(err.Error()).End()
				continue
			}
			jobHistory.IncrSuccess()
		}
	}
	return nil
}
