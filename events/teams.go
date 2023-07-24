package events

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/teams"
	"github.com/google/uuid"
)

var TeamConsumer = EventConsumer{
	WatchEvents: ConsumerTeam,
	HandleFunc:  handleTeamEvents,
	BatchSize:   1,
	Consumers:   1,
	DB:          db.Gorm,
}

func handleTeamEvents(ctx *api.Context, config Config, event api.Event) error {
	switch event.Name {
	case EventTeamUpdate:
		return handleTeamUpdate(ctx, event)
	case EventTeamDelete:
		return handleTeamDelete(ctx, event)
	default:
		return fmt.Errorf("Unrecognized event name: %s", event.Name)
	}
}

// handleTeamDelete makes the necessary changes when a team is deleted.
func handleTeamDelete(ctx *api.Context, event api.Event) error {
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
	teams.PurgeCache(teamID.String())
	return nil
}

// handleTeamUpdate makes the necessary changes when a team spec is updated.
func handleTeamUpdate(ctx *api.Context, event api.Event) error {
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
	teams.PurgeCache(teamID.String())
	return nil
}
