package events

import (
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// handleTeamDelete makes the necessary changes when a team is deleted.
func handleTeamDelete(tx *gorm.DB, event api.Event) error {
	var teamID uuid.UUID
	if _teamID, ok := event.Properties["team_id"]; !ok {
		logger.Warnf("event has invalid property. missing 'team_id'")
		return nil
	} else {
		var err error
		teamID, err = uuid.Parse(_teamID)
		if err != nil {
			logger.Warnf("event has invalid team_id=%q. It's not a UUID", _teamID)
			return nil
		}
	}

	responder.PurgeCache(teamID.String())

	return tx.Delete(&api.Notification{TeamID: teamID}).Error
}

// handleTeamUpdate makes the necessary changes when a team spec is updated.
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

	responder.PurgeCache(teamID.String())

	if team.DeletedAt != nil {
		return tx.Delete(&api.Notification{TeamID: teamID}).Error
	}

	spec, err := team.GetSpec()
	if err != nil {
		return err
	}

	var activeNotificationIDs []string
	for _, n := range spec.Notifications {
		spec, err := collections.StructToJSON(n)
		if err != nil {
			return fmt.Errorf("error converting notification spec to JSON: %w", err)
		}

		var existing []string
		if err := tx.Select("id").Table("notifications").Where("config = ?", spec).Where("team_id = ?", teamID).Where("deleted_at IS NULL").Scan(&existing).Error; err != nil {
			return err
		} else if len(existing) > 0 {
			activeNotificationIDs = append(activeNotificationIDs, existing...)
			continue
		}

		var notification api.Notification
		notification.FromConfig(teamID, n)
		if err := tx.Create(&notification).Error; err != nil {
			return err
		}

		activeNotificationIDs = append(activeNotificationIDs, notification.ID.String())
	}

	return tx.Debug().Model(&api.Notification{}).Where("team_id = ?", teamID).Where("id NOT IN (?)", activeNotificationIDs).Update("deleted_at", gorm.Expr("now()")).Error
}
