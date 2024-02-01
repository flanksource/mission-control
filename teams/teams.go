package teams

import (
	"encoding/json"
	"time"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	dutyModels "github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db/models"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
)

var teamSpecCache = cache.New(time.Hour*1, time.Hour*1)

func GetTeamComponentsFromSelectors(ctx context.Context, teamID uuid.UUID, componentSelectors []types.ResourceSelector) ([]api.TeamComponent, error) {
	var selectedComponents = make(map[string][]uuid.UUID)
	for _, compSelector := range componentSelectors {
		h, _ := hash.JSONMD5Hash(compSelector)
		foundIDs, err := duty.FindComponents(ctx, []types.ResourceSelector{compSelector}, duty.PickColumns("id"))
		if err != nil {
			return nil, err
		}

		selectedComponents[h] = lo.Map(foundIDs, func(c dutyModels.Component, _ int) uuid.UUID { return c.ID })
	}

	var teamComps []api.TeamComponent
	for hash, selectedComps := range selectedComponents {
		for _, compID := range selectedComps {
			teamComps = append(teamComps,
				api.TeamComponent{
					TeamID:      teamID,
					SelectorID:  hash,
					ComponentID: compID,
				},
			)
		}
	}

	return teamComps, nil
}

func GetTeamSpec(ctx context.Context, id string) (*api.TeamSpec, error) {
	if val, found := teamSpecCache.Get(id); found {
		return val.(*api.TeamSpec), nil
	}

	var team models.Team
	if err := ctx.DB().Where("id = ?", id).Find(&team).Error; err != nil {
		return nil, err
	}

	b, err := json.Marshal(team.Spec)
	if err != nil {
		return nil, err
	}

	var teamSpec api.TeamSpec
	if err := json.Unmarshal(b, &teamSpec); err != nil {
		return nil, err
	}

	teamSpecCache.Set(id, &teamSpec, cache.DefaultExpiration)

	return &teamSpec, nil
}

func PurgeCache(id string) {
	teamSpecCache.Delete(id)
}
