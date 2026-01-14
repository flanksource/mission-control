package permission

import (
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
)

func init() {
	events.Register(registerPermissionEvents)
}

func registerPermissionEvents(ctx context.Context) {
	events.RegisterAsyncHandler(handleScopeMaterializationEvents, 1, 1, api.EventScopeMaterialize)
	events.RegisterAsyncHandler(handlePermissionMaterializationEvents, 1, 1, api.EventPermissionMaterialize)
}

func handleScopeMaterializationEvents(ctx context.Context, batch models.Events) models.Events {
	return handleMaterializationEvents(ctx, batch, db.ScopeQueueSourceScope)
}

func handlePermissionMaterializationEvents(ctx context.Context, batch models.Events) models.Events {
	return handleMaterializationEvents(ctx, batch, db.ScopeQueueSourcePermission)
}

func handleMaterializationEvents(ctx context.Context, batch models.Events, sourceType string) models.Events {
	var failed models.Events

	for _, event := range batch {
		action := strings.ToLower(strings.TrimSpace(event.Properties["action"]))
		if action == "" {
			action = db.ScopeQueueActionRebuild
		}

		sourceID := strings.TrimSpace(event.Properties["id"])
		if sourceID == "" {
			event.SetError("materialization event missing id")
			failed = append(failed, event)
			continue
		}

		switch action {
		case db.ScopeQueueActionApply, db.ScopeQueueActionRemove, db.ScopeQueueActionRebuild:
			// ok
		default:
			event.SetError("invalid materialization action")
			failed = append(failed, event)
			continue
		}

		jobRun, err := db.GetProcessScopeJob(ctx, sourceType, sourceID, action)
		if err != nil {
			event.SetError(err.Error())
			failed = append(failed, event)
			continue
		}

		jobRun.Run()
		if jobRun.LastJob != nil {
			if err := jobRun.LastJob.AsError(); err != nil {
				event.SetError(err.Error())
				failed = append(failed, event)
				continue
			}
		}
	}

	return failed
}
