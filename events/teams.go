package events

import (
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
	return nil
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

	responder.PurgeCache(teamID.String())
	return nil
}
