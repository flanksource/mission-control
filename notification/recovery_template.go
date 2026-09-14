package notification

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
)

func recoveryEnvironment(ctx context.Context, episode models.NotificationHealthEpisode, state models.NotificationHealthState) (celVariables, error) {
	env := celVariables{SourceEvent: episode.ResourceType + ".healthy"}
	if state.HealthySince != nil {
		env.EventTime = *state.HealthySince
	}
	var agentID uuid.UUID
	switch episode.ResourceType {
	case "config":
		env.ConfigItem = &models.ConfigItem{}
		if err := ctx.DB().Where("id = ?", episode.ResourceID).First(env.ConfigItem).Error; err != nil {
			return env, err
		}
		agentID = env.ConfigItem.AgentID
		env.Permalink = fmt.Sprintf("%s/catalog/%s", api.FrontendURL, episode.ResourceID)
	case "component":
		env.Component = &models.Component{}
		if err := ctx.DB().Where("id = ?", episode.ResourceID).First(env.Component).Error; err != nil {
			return env, err
		}
		agentID = env.Component.AgentID
		env.Permalink = fmt.Sprintf("%s/topology/%s", api.FrontendURL, episode.ResourceID)
	case "check":
		env.SourceEvent = "check.passed"
		env.Check = &models.Check{}
		if err := ctx.DB().Where("id = ?", episode.ResourceID).First(env.Check).Error; err != nil {
			return env, err
		}
		env.Check.Status = models.CheckHealthStatus(state.Health)
		env.Canary = &models.Canary{}
		if err := ctx.DB().Where("id = ?", env.Check.CanaryID).First(env.Canary).Error; err != nil {
			return env, err
		}
		env.CheckStatus = &models.CheckStatus{}
		if err := ctx.DB().Where("check_id = ?", episode.ResourceID).Order("time DESC").Limit(1).Find(env.CheckStatus).Error; err != nil {
			return env, err
		}
		agentID = env.Check.AgentID
		env.Permalink = fmt.Sprintf("%s/health?layout=table&checkId=%s&timeRange=1h", api.FrontendURL, episode.ResourceID)
	}
	if agentID != uuid.Nil {
		env.Agent = &models.Agent{}
		if err := ctx.DB().Where("id = ?", agentID).First(env.Agent).Error; err != nil {
			return env, err
		}
	}
	env.SetSilenceURL(api.FrontendURL)
	return env, nil
}
