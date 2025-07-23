package notification

import (
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
)

type celVariables struct {
	Agent   *models.Agent
	Channel string

	ConfigItem  *models.ConfigItem
	Component   *models.Component
	CheckStatus *models.CheckStatus
	Check       *models.Check
	Canary      *models.Canary

	// For notifications related to config items
	// we attach the most recent config changes (last 1h)
	RecentEvents []string

	Incident   *models.Incident
	Responder  *models.Responder
	Evidence   *models.Evidence
	Hypothesis *models.Hypothesis

	Comment *models.Comment
	Author  *models.Person

	NewState   string
	Permalink  string
	SilenceURL string

	Summary models.NotificationSummary

	GroupedResources []string
}

func (t *celVariables) SetSilenceURL(frontendURL string) {
	baseURL := fmt.Sprintf("%s/notifications/silences/add", frontendURL)

	switch {
	case t.ConfigItem != nil:
		t.SilenceURL = fmt.Sprintf("%s?config_id=%s", baseURL, t.ConfigItem.ID.String())
	case t.Component != nil:
		t.SilenceURL = fmt.Sprintf("%s?component_id=%s", baseURL, t.Component.ID.String())
	case t.Check != nil:
		t.SilenceURL = fmt.Sprintf("%s?check_id=%s", baseURL, t.Check.ID.String())
	case t.Canary != nil:
		t.SilenceURL = fmt.Sprintf("%s?canary_id=%s", baseURL, t.Canary.ID.String())
	}
}

type ResourceHealthRow struct {
	Health    models.Health
	DeletedAt *time.Time
	UpdatedAt *time.Time
}

func (t *celVariables) GetResourceCurrentHealth(ctx context.Context) (ResourceHealthRow, error) {
	var err error
	var row ResourceHealthRow
	switch {
	case t.ConfigItem != nil:
		err = ctx.DB().Model(&models.ConfigItem{}).Select("health, deleted_at", "updated_at").Where("id = ?", t.ConfigItem.ID).Scan(&row).Error
	case t.Component != nil:
		err = ctx.DB().Model(&models.Component{}).Select("health, deleted_at", "updated_at").Where("id = ?", t.Component.ID).Scan(&row).Error
	case t.Check != nil:
		err = ctx.DB().Model(&models.Check{}).Select("status, deleted_at", "updated_at").Where("id = ?", t.Check.ID).Scan(&row).Error
	default:
		return ResourceHealthRow{}, errors.New("no resource")
	}

	if row.Health == "" {
		row.Health = models.HealthUnknown
	}

	return row, err
}

func (t *celVariables) AsMap(ctx context.Context) map[string]any {
	output := map[string]any{
		"permalink":  t.Permalink,
		"silenceURL": t.SilenceURL,
		"channel":    t.Channel,

		"agent":        lo.FromPtr(t.Agent).AsMap(),
		"check_status": lo.FromPtr(t.CheckStatus).AsMap(),
		"check":        lo.FromPtr(t.Check).AsMap("spec"),
		"config":       lo.FromPtr(t.ConfigItem).AsMap("spec"),
		"canary":       lo.FromPtr(t.Canary).AsMap("spec"),
		"component": lo.ToPtr(lo.FromPtr(t.Component)).
			AsMap("checks", "incidents", "analysis", "components", "order", "relationship_id", "children", "parents"),
		"recent_events": t.RecentEvents,

		"evidence":   lo.FromPtr(t.Evidence).AsMap(),
		"hypothesis": lo.FromPtr(t.Hypothesis).AsMap(),
		"incident":   lo.FromPtr(t.Incident).AsMap(),
		"responder":  lo.FromPtr(t.Responder).AsMap(),

		"comment": lo.FromPtr(t.Comment).AsMap(),
		"author":  lo.FromPtr(t.Author).AsMap(),

		"summary": t.Summary.AsMap(),
	}

	if len(t.GroupedResources) > 0 {
		output["groupedResources"] = t.GroupedResources
	}

	if t.NewState != "" {
		output["new_state"] = t.NewState
	}

	resourceContext := duty.GetResourceContext(ctx, t.SelectableResource())
	return collections.MergeMap(resourceContext, output)
}

func (t *celVariables) SelectableResource() types.ResourceSelectable {
	if t.Component != nil {
		return t.Component
	}
	if t.ConfigItem != nil {
		return t.ConfigItem
	}
	if t.Check != nil {
		return t.Check
	}
	if t.Canary != nil {
		return t.Canary
	}
	return nil
}
