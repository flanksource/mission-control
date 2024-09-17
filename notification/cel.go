package notification

import (
	"errors"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
)

type celVariables struct {
	Agent *models.Agent

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

	NewState  string
	Permalink string
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

func (t *celVariables) AsMap() map[string]any {
	output := map[string]any{
		"permalink": t.Permalink,
	}

	if t.Agent != nil {
		output["agent"] = t.Agent.AsMap()
	}

	if t.ConfigItem != nil {
		output["config"] = t.ConfigItem.AsMap("last_scraped_time", "path", "parent_id")
	}

	if t.NewState != "" {
		output["new_state"] = t.NewState
	}
	if t.Component != nil {
		output["component"] = t.Component.AsMap("checks", "incidents", "analysis", "components", "order", "relationship_id", "children", "parents")
	}

	if t.CheckStatus != nil {
		output["status"] = t.CheckStatus.AsMap()
	}
	if t.Check != nil {
		output["check"] = t.Check.AsMap("spec")
	}
	if t.Canary != nil {
		output["canary"] = t.Canary.AsMap("spec")
	}

	if t.Incident != nil {
		output["incident"] = t.Incident.AsMap()
	}
	if t.Responder != nil {
		output["responder"] = t.Responder.AsMap()
	}
	if t.Evidence != nil {
		output["evidence"] = t.Evidence.AsMap()
	}
	if t.Hypothesis != nil {
		output["hypothesis"] = t.Hypothesis.AsMap()
	}

	if t.Comment != nil {
		output["comment"] = t.Comment.AsMap()
	}
	if t.Author != nil {
		output["author"] = t.Author.AsMap()
	}

	return output
}
