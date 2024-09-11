package notification

import (
	"errors"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"
)

type SilenceSaveRequest struct {
	models.NotificationSilenceResource
	From        time.Time `json:"from"`
	Until       time.Time `json:"until"`
	Duration    string    `json:"duration"`
	Description string    `json:"description"`
}

func (t *SilenceSaveRequest) Validate() error {
	if t.From.IsZero() {
		return errors.New("`from` time is required")
	}

	if t.Until.IsZero() {
		if t.Duration == "" {
			return errors.New("`until` or `duration` is required")
		}

		if parsed, err := duration.ParseDuration(t.Duration); err != nil {
			return err
		} else {
			t.Until = t.From.Add(time.Duration(parsed))
		}
	}

	if t.From.After(t.Until) {
		return errors.New("`from` time must be before `until` time")
	}

	if t.NotificationSilenceResource.CanaryID == nil && t.NotificationSilenceResource.CheckID == nil && t.NotificationSilenceResource.ConfigID == nil &&
		t.NotificationSilenceResource.ComponentID == nil {
		return errors.New("at least one of `config_id`, `canary_id`, `check_id` or `component_id` is required")
	}

	return nil
}

func SaveNotificationSilence(ctx context.Context, req SilenceSaveRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	silence := models.NotificationSilence{
		NotificationSilenceResource: req.NotificationSilenceResource,
		From:                        req.From,
		Until:                       req.Until,
		Description:                 req.Description,
		Source:                      models.SourceUI,
		CreatedBy:                   lo.ToPtr(ctx.User().ID),
	}

	return ctx.DB().Create(&silence).Error
}

func getSilencedResourceFromCelEnv(celEnv map[string]any) models.NotificationSilenceResource {
	var silencedResource models.NotificationSilenceResource
	if v, ok := celEnv["config"]; ok {
		if vv, ok := v.(map[string]any); ok {
			silencedResource.ConfigID = lo.ToPtr(vv["id"].(string))
		}
	}

	if v, ok := celEnv["check"]; ok {
		if vv, ok := v.(map[string]any); ok {
			silencedResource.CheckID = lo.ToPtr(vv["id"].(string))
		}
	}

	if v, ok := celEnv["canary"]; ok {
		if vv, ok := v.(map[string]any); ok {
			silencedResource.CanaryID = lo.ToPtr(vv["id"].(string))
		}
	}

	if v, ok := celEnv["component"]; ok {
		if vv, ok := v.(map[string]any); ok {
			silencedResource.ComponentID = lo.ToPtr(vv["id"].(string))
		}
	}

	return silencedResource
}
