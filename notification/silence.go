package notification

import (
	"errors"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/db"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"
	"github.com/timberio/go-datemath"
)

type SilenceSaveRequest struct {
	models.NotificationSilenceResource
	From        string `json:"from"`
	Until       string `json:"until"`
	Description string `json:"description"`
	Recursive   bool   `json:"recursive"`

	from  time.Time
	until time.Time
}

func (t *SilenceSaveRequest) Validate() error {
	if t.From == "" {
		return errors.New("`from` time is required")
	}

	if t.Until == "" {
		return errors.New("`until` is required")
	}

	if parsedTime, err := datemath.ParseAndEvaluate(t.From); err != nil {
		return err
	} else {
		t.from = parsedTime
	}

	if parsedTime, err := datemath.ParseAndEvaluate(t.Until); err != nil {
		return err
	} else {
		t.until = parsedTime
	}

	if t.from.After(t.until) {
		return errors.New("`from` time must be before `until")
	}

	if t.NotificationSilenceResource.Empty() {
		return errors.New("at least one of `config_id`, `canary_id`, `check_id` or `component_id` is required")
	}

	return nil
}

func SaveNotificationSilence(ctx context.Context, req SilenceSaveRequest) error {
	if err := req.Validate(); err != nil {
		return api.Errorf(api.EINVALID, err.Error())
	}

	silence := models.NotificationSilence{
		NotificationSilenceResource: req.NotificationSilenceResource,
		From:                        req.from,
		Until:                       req.until,
		Description:                 req.Description,
		Recursive:                   req.Recursive,
		Source:                      models.SourceUI,
		CreatedBy:                   lo.ToPtr(ctx.User().ID),
	}

	return db.ErrorDetails(ctx.DB().Create(&silence).Error)
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
