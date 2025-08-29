package events

import (
	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
)

type EventResource struct {
	Component    *models.Component    `json:"component,omitempty"`
	Config       *models.ConfigItem   `json:"config,omitempty"`
	Check        *models.Check        `json:"check,omitempty"`
	CheckSummary *models.CheckSummary `json:"check_summary,omitempty"`
	Canary       *models.Canary       `json:"canary,omitempty"`
}

func (t *EventResource) AsMap() map[string]any {
	output := map[string]any{}

	if t.Component != nil {
		output["component"] = t.Component.AsMap()
	}
	if t.Config != nil {
		output["config"] = t.Config.AsMap()
	}
	if t.Check != nil {
		output["check"] = t.Check.AsMap()
	}
	if t.Canary != nil {
		output["canary"] = t.Canary.AsMap()
	}
	if t.CheckSummary != nil {
		output["check_summary"] = t.CheckSummary.AsMap()
	}

	return output
}

// buildEventResource creates an EventResource from an event by fetching the appropriate models from the database
func BuildEventResource(ctx context.Context, event models.Event) (EventResource, error) {
	var eventResource EventResource
	switch event.Name {
	case api.EventCheckFailed, api.EventCheckPassed:
		checkID := event.EventID
		if err := ctx.DB().Where("id = ?", checkID).First(&eventResource.Check).Error; err != nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "check(id=%s) not found", checkID)
		}

		if summary, err := duty.CheckSummary(ctx, checkID.String()); err != nil {
			return eventResource, err
		} else if summary != nil {
			eventResource.CheckSummary = summary
		}

		if err := ctx.DB().Where("id = ?", eventResource.Check.CanaryID).First(&eventResource.Canary).Error; err != nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "canary(id=%s) not found", eventResource.Check.CanaryID)
		}

	case api.EventComponentHealthy, api.EventComponentUnhealthy, api.EventComponentWarning, api.EventComponentUnknown:
		if err := ctx.DB().Model(&models.Component{}).Where("id = ?", event.EventID).First(&eventResource.Component).Error; err != nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "component(id=%s) not found", event.EventID)
		}

	case api.EventConfigHealthy, api.EventConfigUnhealthy, api.EventConfigWarning, api.EventConfigUnknown, api.EventConfigDegraded:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.EventID).First(&eventResource.Config).Error; err != nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.EventID)
		}

	case api.EventConfigCreated, api.EventConfigDeleted:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.EventID).First(&eventResource.Config).Error; err != nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.EventID)
		}

	case api.EventConfigChanged, api.EventConfigUpdated:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["config_id"]).First(&eventResource.Config).Error; err != nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["config_id"])
		}
	}
	return eventResource, nil
}
