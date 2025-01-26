package notification

import (
	"errors"
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/db"
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

func (t *celVariables) GetResourceHealth(ctx context.Context) (models.Health, error) {
	health := models.HealthUnknown
	var err error

	switch {
	case t.ConfigItem != nil:
		err = ctx.DB().Model(&models.ConfigItem{}).Select("health").Where("id = ?", t.ConfigItem.ID).Scan(&health).Error
	case t.Component != nil:
		err = ctx.DB().Model(&models.Component{}).Select("health").Where("id = ?", t.Component.ID).Scan(&health).Error
	case t.Check != nil:
		err = ctx.DB().Model(&models.Check{}).Select("status").Where("id = ?", t.Check.ID).Scan(&health).Error
	default:
		return models.HealthUnknown, errors.New("no resource")
	}

	return health, err
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

	// Placeholders for commonly used fields of the resource
	// If a resource exists, they'll be filled up below
	output["name"] = ""
	output["status"] = ""
	output["health"] = ""
	output["labels"] = map[string]string{}
	tags := map[string]string{}

	if resource := t.SelectableResource(); resource != nil {
		// set the alias name/status/health/labels/tags of the resource
		output["name"] = resource.GetName()
		if status, err := resource.GetStatus(); err == nil {
			output["status"] = status
		}
		if health, err := resource.GetHealth(); err == nil {
			output["health"] = health
		}
		if table, ok := resource.(models.TaggableModel); ok {
			tags = table.GetTags()
		}
		if table, ok := resource.(models.LabelableModel); ok {
			output["labels"] = table.GetLabels()
		}
	}

	if ctx.DB() != nil {
		if tags, err := db.GetDistinctTags(ctx); err != nil {
			logger.Errorf("failed to get distinct tags for notification cel variable: %w", err)
		} else {
			for _, tag := range tags {
				if _, ok := output[tag]; !ok {
					output[tag] = ""
				}
			}
		}
	}

	output["tags"] = tags

	// Inject tags as top level variables
	for k, v := range tags {
		if !gomplate.IsValidCELIdentifier(k) {
			logger.V(9).Infof("skipping tag %s as it is not a valid CEL identifier", k)
			continue
		}

		if _, ok := output[k]; ok {
			logger.V(9).Infof("skipping tag %s as it already exists in the playbook template environment", k)
			continue
		}

		output[k] = v
	}

	return output
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
