package events

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events/eventconsumer"
	"github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/teams"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func NewTeamConsumerSync(db *gorm.DB, pool *pgxpool.Pool) *eventconsumer.EventConsumer {
	return eventconsumer.New(db, pool, eventQueueUpdateChannel, newEventQueueSyncConsumerFunc(syncConsumerWatchEvents["team"], handleTeamEvent))
}

func handleTeamEvent(ctx *api.Context, event api.Event) error {
	switch event.Name {
	case EventTeamUpdate:
		return handleTeamUpdate(ctx, event)
	case EventTeamDelete:
		return handleTeamDelete(ctx, event)
	default:
		return fmt.Errorf("unrecognized event name: %s", event.Name)
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
