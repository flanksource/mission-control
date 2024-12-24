package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/db"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/timberio/go-datemath"
)

type SilenceSaveRequest struct {
	models.NotificationSilenceResource

	Name        string                   `json:"name"`
	From        *string                  `json:"from,omitempty"`
	Until       *string                  `json:"until,omitempty"`
	Description *string                  `json:"description,omitempty"`
	Recursive   bool                     `json:"recursive"`
	Filter      types.CelExpression      `json:"filter"`
	Selectors   []types.ResourceSelector `json:"selectors"`

	ID        uuid.UUID `json:"-"`
	Namespace string    `json:"-"`
	Source    string    `json:"-"`

	from  *time.Time
	until *time.Time
}

func (t *SilenceSaveRequest) Validate() error {
	if t.From != nil {
		if parsedTime, err := datemath.ParseAndEvaluate(*t.From); err != nil {
			return err
		} else {
			t.from = &parsedTime
		}
	}

	if t.Until != nil {
		if parsedTime, err := datemath.ParseAndEvaluate(*t.Until); err != nil {
			return err
		} else {
			t.until = &parsedTime
		}
	}

	if t.from != nil && t.until != nil {
		if t.from.After(*t.until) {
			return errors.New("`from` time must be before `until")
		}
	}

	if t.NotificationSilenceResource.Empty() && t.Filter == "" && len(t.Selectors) == 0 {
		return errors.New("at least one of config_id, canary_id, check_id, component_id, filter or selectors is required")
	}

	return nil
}

func SaveNotificationSilence(ctx context.Context, req SilenceSaveRequest) error {
	if err := req.Validate(); err != nil {
		return api.Errorf(api.EINVALID, "%s", err)
	}

	silence := models.NotificationSilence{
		NotificationSilenceResource: req.NotificationSilenceResource,
		Description:                 req.Description,
		ID:                          req.ID,
		Name:                        req.Name,
		Namespace:                   req.Namespace,
		From:                        req.from,
		Filter:                      req.Filter,
		Until:                       req.until,
		Recursive:                   req.Recursive,
		Source:                      req.Source,
	}

	if len(req.Selectors) > 0 {
		selectorsRaw, err := json.Marshal(req.Selectors)
		if err != nil {
			return api.Errorf(api.EINVALID, "%s", err.Error())
		}
		silence.Selectors = selectorsRaw
	}

	if ctx.User() != nil {
		silence.CreatedBy = lo.ToPtr(ctx.User().ID)
	}

	return db.ErrorDetails(ctx.DB().Save(&silence).Error)
}

func PersistNotificationSilenceFromCRD(ctx context.Context, obj *v1.NotificationSilence) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}

	request := SilenceSaveRequest{
		ID:          uid,
		Name:        obj.ObjectMeta.Name,
		Namespace:   obj.ObjectMeta.Namespace,
		Description: obj.Spec.Description,
		From:        obj.Spec.From,
		Until:       obj.Spec.Until,
		Source:      models.SourceCRD,
		Filter:      obj.Spec.Filter,
		Selectors:   obj.Spec.Selectors,
		Recursive:   obj.Spec.Recursive,
	}

	return SaveNotificationSilence(ctx, request)
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
