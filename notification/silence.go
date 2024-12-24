package notification

import (
	"errors"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/db"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
	"github.com/timberio/go-datemath"
)

type SilenceSaveRequest struct {
	models.NotificationSilenceResource
	From        string              `json:"from"`
	Until       string              `json:"until"`
	Description string              `json:"description"`
	Recursive   bool                `json:"recursive"`
	Filter      types.CelExpression `json:"filter"`

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

	if t.NotificationSilenceResource.Empty() && t.Filter == "" {
		return errors.New("at least one of config_id, canary_id, check_id, component_id, filter is required")
	}

	return nil
}

func SaveNotificationSilence(ctx context.Context, req SilenceSaveRequest) error {
	if err := req.Validate(); err != nil {
		return api.Errorf(api.EINVALID, "%s", err)
	}

	silence := models.NotificationSilence{
		NotificationSilenceResource: req.NotificationSilenceResource,
		From:                        req.from,
		Filter:                      req.Filter,
		Until:                       req.until,
		Description:                 req.Description,
		Recursive:                   req.Recursive,
		Source:                      models.SourceUI,
		CreatedBy:                   lo.ToPtr(ctx.User().ID),
	}

	return db.ErrorDetails(ctx.DB().Create(&silence).Error)
}

func getSilencedResourceFromCelEnv(celEnv *celVariables) models.NotificationSilenceResource {
	var silencedResource models.NotificationSilenceResource
	if celEnv.ConfigItem != nil {
		silencedResource.ConfigID = lo.ToPtr(celEnv.ConfigItem.ID.String())
	}

	if celEnv.Check != nil {
		silencedResource.CheckID = lo.ToPtr(celEnv.Check.ID.String())
	}

	if celEnv.Canary != nil {
		silencedResource.CanaryID = lo.ToPtr(celEnv.Canary.ID.String())
	}

	if celEnv.Component != nil {
		silencedResource.ComponentID = lo.ToPtr(celEnv.Component.ID.String())
	}

	return silencedResource
}
