package notification

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/db"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
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

func DeleteStaleNotificationSilence(ctx context.Context, newer *v1.NotificationSilence) error {
	return ctx.DB().Model(&models.NotificationSilence{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("deleted_at IS NULL").
		Update("deleted_at", time.Now()).Error
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

func CanSilenceViaSelectors(ctx context.Context, n []models.NotificationSendHistory, selectors types.ResourceSelectors) ([]models.NotificationSendHistory, error) {
	var hasConfig, hasComponent, hasCheck bool
	notifResourceID := make(map[uuid.UUID][]models.NotificationSendHistory)
	for _, notif := range n {
		notifResourceID[notif.ResourceID] = append(notifResourceID[notif.ResourceID], notif)
		switch strings.Split(notif.SourceEvent, ".")[0] {
		case "config":
			hasConfig = true
		case "component":
			hasComponent = true
		case "check":
			hasCheck = true
		}
	}
	var silenced []models.NotificationSendHistory
	if hasConfig {
		ids, err := query.FindConfigIDsByResourceSelector(ctx, -1, selectors...)
		if err != nil {
			return nil, fmt.Errorf("error querying configs for selector[%v]: %w", selectors, err)
		}
		for _, id := range ids {
			if n, ok := notifResourceID[id]; ok {
				silenced = append(silenced, n...)
			}
		}
	}
	if hasComponent {
		ids, err := query.FindComponentIDs(ctx, -1, selectors...)
		if err != nil {
			return nil, fmt.Errorf("error querying components for selector[%v]: %w", selectors, err)
		}
		for _, id := range ids {
			if n, ok := notifResourceID[id]; ok {
				silenced = append(silenced, n...)
			}
		}
	}
	if hasCheck {
		ids, err := query.FindCheckIDs(ctx, -1, selectors...)
		if err != nil {
			return nil, fmt.Errorf("error querying checks for selector[%v]: %w", selectors, err)
		}
		for _, id := range ids {
			if n, ok := notifResourceID[id]; ok {
				silenced = append(silenced, n...)
			}
		}
	}
	return silenced, nil
}

func CanSilenceViaResourceID(n []models.NotificationSendHistory, resourceID string) []models.NotificationSendHistory {
	var silenced []models.NotificationSendHistory
	for _, notif := range n {
		if notif.ResourceID.String() == resourceID {
			silenced = append(silenced, notif)
		}
	}
	return silenced
}

func CanSilenceViaFilter(ctx context.Context, n []models.NotificationSendHistory, filter string) ([]models.NotificationSendHistory, error) {
	var silenced []models.NotificationSendHistory
	for _, notif := range n {
		eventPropsRaw := notif.Payload["properties"]
		var properties types.JSONStringMap
		decodedBytes, err := base64.StdEncoding.DecodeString(eventPropsRaw)
		if err != nil {
			return nil, fmt.Errorf("error decoding from base64: %w", err)
		}
		if err := json.Unmarshal(decodedBytes, &properties); err != nil {
			return nil, fmt.Errorf("error unmarshaling json: %w", err)
		}

		event := models.Event{
			Name:       notif.SourceEvent,
			Properties: properties,
		}
		celEnv, err := GetEnvForEvent(ctx, event)
		if err != nil {
			return nil, fmt.Errorf("error getting env for event: %w", err)
		}

		res, err := ctx.RunTemplate(gomplate.Template{Expression: string(filter)}, celEnv.AsMap(ctx))
		if err != nil {
			return nil, fmt.Errorf("error in templating: %w", err)
		}

		if ok, _ := strconv.ParseBool(res); ok {
			silenced = append(silenced, notif)
		}
	}
	return silenced, nil
}

func GetResourceAsMapFromEvent(ctx context.Context, event, id string) (map[string]any, error) {
	var c gomplate.AsMapper
	var err error
	switch strings.Split(event, ".")[0] {
	case "config":
		c, err = query.ConfigItemFromCache(ctx, id)
	case "component":
		var g models.Component
		g, err = query.ComponentFromCache(ctx, id, true)
		c = &g
	case "check":
		c, err = query.FindCachedCheck(ctx, id)
	}
	if c == nil || err != nil {
		return nil, err
	}
	return c.AsMap("spec", "config"), nil
}
