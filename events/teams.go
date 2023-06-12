package events

import (
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func handleTeamDelete(tx *gorm.DB, event api.Event) error {
	var teamID uuid.UUID
	if _teamID, ok := event.Properties["team_id"]; !ok {
		logger.Warnf("event has invalid property. missing 'team_id'")
		return nil
	} else {
		var err error
		teamID, err = uuid.Parse(_teamID)
		if err != nil {
			return err
		}
	}

	return tx.Delete(&api.Notification{TeamID: teamID}).Error
}

func handleTeamUpdate(tx *gorm.DB, event api.Event) error {
	var teamID uuid.UUID
	if _teamID, ok := event.Properties["team_id"]; !ok {
		logger.Warnf("event has invalid property. missing 'team_id'")
		return nil
	} else {
		var err error
		teamID, err = uuid.Parse(_teamID)
		if err != nil {
			return err
		}
	}

	var team api.Team
	if err := tx.Where("id = ?", teamID).First(&team).Error; err != nil {
		return err
	}

	spec, err := team.GetSpec()
	if err != nil {
		return err
	}

	for _, n := range spec.Notifications {
		// 1. Find an existing notification that's active of this team from the notifications table
		spec, err := collections.StructToJSON(n)
		if err != nil {
			return fmt.Errorf("error converting scraper spec to JSON: %w", err)
		}

		var existing []api.Notification
		if err := tx.Where("config = ? AND team_id = ? AND deleted_at IS NULL", spec, teamID).Find(&existing).Error; err != nil {
			return err
		}

		// 2. If it finds the notification then skip
		if len(existing) > 0 {
			continue
		}

		// 3. Else create it
		var notification api.Notification
		notification.FromConfig(teamID, n)
		if err := tx.Create(&notification).Error; err != nil {
			return err
		}
	}

	// TODO: All the notifications of this team that weren't updated just now should be deleted

	return nil
}
