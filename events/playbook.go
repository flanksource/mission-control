package events

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
)

type FilterEnv struct {
	Component *models.Component `json:"component,omitempty"`
	Check     *models.Check     `json:"check,omitempty"`
}

func SavePlaybookRun(ctx *api.Context, event api.Event) error {
	playbooks, err := FindPlaybooksListeningOnEvent(ctx, event.Name)
	if err != nil {
		return fmt.Errorf("error fetching playbooks: %w", err)
	}

	var eventResource FilterEnv
	switch event.Name {
	case EventCheckFailed, EventCheckPassed:
		if err := ctx.DB().Model(&models.Check{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Check).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "check(id=%s) not found", event.Properties["id"])
		}

	case EventComponentStatusHealthy, EventComponentStatusUnhealthy, EventComponentStatusInfo, EventComponentStatusWarning, EventComponentStatusError:
		if err := ctx.DB().Model(&models.Component{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Component).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "component(id=%s) not found", event.Properties["id"])
		}
	}

	logger.Infof("Resources: %v", eventResource)

	for _, p := range playbooks {
		logger.Infof("Found playbook %s", p.Name)
		// If playbook passes the filterm then save playbook run
	}

	return nil
}

// TODO: Need to cache results
func FindPlaybooksListeningOnEvent(ctx *api.Context, event string) ([]models.Playbook, error) {
	specEvent, ok := eventToSpecEvent[event]
	if !ok {
		return nil, nil
	}

	var playbooks []models.Playbook
	query := fmt.Sprintf(`SELECT * FROM playbooks WHERE spec->'on'->'%s' @> '[{"event": "%s"}]'`, specEvent.Class, specEvent.Event)
	if err := ctx.DB().Debug().Raw(query).Scan(&playbooks).Error; err != nil {
		return nil, err
	}

	return playbooks, nil
}

type PlaybookSpecEvent struct {
	Class string // canary or component
	Event string // varies depending on the type
}

var eventToSpecEvent = map[string]PlaybookSpecEvent{
	EventCheckPassed:              {"canary", "passed"},
	EventCheckFailed:              {"canary", "failed"},
	EventComponentStatusHealthy:   {"canary", "healthy"},
	EventComponentStatusUnhealthy: {"canary", "unhealthy"},
	EventComponentStatusInfo:      {"canary", "info"},
	EventComponentStatusWarning:   {"canary", "warning"},
	EventComponentStatusError:     {"canary", "error"},
}
