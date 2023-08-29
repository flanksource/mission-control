package events

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
)

func SavePlaybookRun(ctx *api.Context, event api.Event) error {
	playbooks, err := FindPlaybooksListeningOnEvent(ctx, event.Name)
	if err != nil {
		return fmt.Errorf("error fetching playbooks: %w", err)
	}

	var eventResource any
	switch event.Name {
	case EventCheckFailed, EventCheckPassed:
		if err := ctx.DB().Model(&models.Check{}).Where("id = ?", event.Properties["id"]).First(&eventResource).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "check(id=%s) not found", event.Properties["id"])
		}

	case EventComponentStatusHealthy, EventComponentStatusUnhealthy, EventComponentStatusInfo, EventComponentStatusWarning, EventComponentStatusError:
		if err := ctx.DB().Model(&models.Component{}).Where("id = ?", event.Properties["id"]).First(&eventResource).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "component(id=%s) not found", event.Properties["id"])
		}
	}

	for _, p := range playbooks {
		logger.Infof("Found playbook %s", p.Name)
		// If playbook passes the filterm then save playbook run
	}

	return nil
}

// TODO: Need to cache results
func FindPlaybooksListeningOnEvent(ctx *api.Context, event string) ([]models.Playbook, error) {
	return nil, nil
}
