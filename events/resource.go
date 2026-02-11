package events

import (
	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

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

// BuildEventResource creates an EventResource from an event by fetching the appropriate models from the database
func BuildEventResource(ctx context.Context, event models.Event) (EventResource, error) {
	var eventResource EventResource
	switch event.Name {
	case api.EventCheckFailed, api.EventCheckPassed:
		checkID := event.EventID
		var check models.Check
		if err := ctx.DB().Where("id = ?", checkID).Limit(1).Find(&check).Error; err != nil {
			return eventResource, err
		}
		if check.ID == uuid.Nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "check(id=%s) not found", checkID)
		}
		eventResource.Check = &check

		if summary, err := duty.CheckSummary(ctx, checkID.String()); err != nil {
			return eventResource, err
		} else if summary != nil {
			eventResource.CheckSummary = summary
		}

		var canary models.Canary
		if err := ctx.DB().Where("id = ?", eventResource.Check.CanaryID).Limit(1).Find(&canary).Error; err != nil {
			return eventResource, err
		}
		if canary.ID == uuid.Nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "canary(id=%s) not found", eventResource.Check.CanaryID)
		}
		eventResource.Canary = &canary

	case api.EventComponentHealthy, api.EventComponentUnhealthy, api.EventComponentWarning, api.EventComponentUnknown:
		var component models.Component
		if err := ctx.DB().Model(&models.Component{}).Where("id = ?", event.EventID).Limit(1).Find(&component).Error; err != nil {
			return eventResource, err
		}
		if component.ID == uuid.Nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "component(id=%s) not found", event.EventID)
		}
		eventResource.Component = &component

	case api.EventConfigHealthy, api.EventConfigUnhealthy, api.EventConfigWarning, api.EventConfigUnknown, api.EventConfigDegraded:
		var config models.ConfigItem
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.EventID).Limit(1).Find(&config).Error; err != nil {
			return eventResource, err
		}
		if config.ID == uuid.Nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.EventID)
		}
		eventResource.Config = &config

	case api.EventConfigCreated, api.EventConfigDeleted:
		var config models.ConfigItem
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.EventID).Limit(1).Find(&config).Error; err != nil {
			return eventResource, err
		}
		if config.ID == uuid.Nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.EventID)
		}
		eventResource.Config = &config

	case api.EventConfigChanged, api.EventConfigUpdated:
		var config models.ConfigItem
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["config_id"]).Limit(1).Find(&config).Error; err != nil {
			return eventResource, err
		}
		if config.ID == uuid.Nil {
			return eventResource, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["config_id"])
		}
		eventResource.Config = &config
	}
	return eventResource, nil
}
