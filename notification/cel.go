package notification

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
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

	SourceEvent string
	EventTime   time.Time

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

func (t celVariables) WithNotificationRef(notificationID string) celVariables {
	t.Permalink = appendRefNotification(t.Permalink, notificationID)
	return t
}

func appendRefNotification(rawURL string, notificationID string) string {
	if rawURL == "" || notificationID == "" {
		return rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	query := u.Query()
	query.Set("refNotification", notificationID)
	u.RawQuery = query.Encode()

	return u.String()
}

type ResourceHealthRow struct {
	Health    models.Health
	Status    string
	DeletedAt *time.Time
	UpdatedAt *time.Time
}

func (t *celVariables) GetResourceCurrentHealthStatus(ctx context.Context) (ResourceHealthRow, error) {
	var err error
	var row ResourceHealthRow
	switch {
	case t.ConfigItem != nil:
		err = ctx.DB().Model(&models.ConfigItem{}).Select("health", "status", "deleted_at", "updated_at").Where("id = ?", t.ConfigItem.ID).Scan(&row).Error
	case t.Component != nil:
		err = ctx.DB().Model(&models.Component{}).Select("health", "status", "deleted_at", "updated_at").Where("id = ?", t.Component.ID).Scan(&row).Error
	case t.Check != nil:
		err = ctx.DB().Model(&models.Check{}).Select("status AS health", "status", "deleted_at", "updated_at").Where("id = ?", t.Check.ID).Scan(&row).Error
	default:
		return ResourceHealthRow{}, errors.New("no resource")
	}

	if row.Health == "" {
		row.Health = models.HealthUnknown
	}

	return row, err
}

type celAsMapOption string

var (
	celVarGetLatestHealthStatus celAsMapOption = "latestHealthStatus"
)

func (t *celVariables) AsMap(ctx context.Context, opts ...celAsMapOption) map[string]any {
	output := map[string]any{
		"permalink":    t.Permalink,
		"silenceURL":   t.SilenceURL,
		"channel":      t.Channel,
		"source_event": t.SourceEvent,
		"event_time":   t.EventTime,

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
	}

	if len(t.GroupedResources) > 0 {
		output["groupedResources"] = t.GroupedResources
	}

	if t.NewState != "" {
		output["new_state"] = t.NewState
	}

	resourceContext := duty.GetResourceContext(ctx, t.SelectableResource())
	if ctx.DB() != nil && slices.Contains(opts, celVarGetLatestHealthStatus) {
		if r, err := t.GetResourceCurrentHealthStatus(ctx); err == nil {
			t.setResourceCurrentHealthStatus(resourceContext, output, r)
		}
	}
	return collections.MergeMap(resourceContext, output)
}

func (t *celVariables) setResourceCurrentHealthStatus(resourceContext, output map[string]any, r ResourceHealthRow) {
	resourceContext["health"] = r.Health
	resourceContext["status"] = r.Status

	switch {
	case t.ConfigItem != nil:
		setResourceMapCurrentHealthStatus(output, "config", r)
	case t.Component != nil:
		setResourceMapCurrentHealthStatus(output, "component", r)
	case t.Check != nil:
		setResourceMapCurrentHealthStatus(output, "check", r)
	}
}

func setResourceMapCurrentHealthStatus(output map[string]any, key string, r ResourceHealthRow) {
	if resourceMap, ok := output[key].(map[string]any); ok {
		resourceMap["health"] = r.Health
		resourceMap["status"] = r.Status
	}
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
