package teams

import (
	"encoding/json"
	"time"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
)

var teamSpecCache = utils.NewGenCache(time.Hour*1, time.Hour*1)

func GetTeamComponentsFromSelectors(ctx context.Context, teamID uuid.UUID, componentSelectors []types.ResourceSelector) ([]api.TeamComponent, error) {
	var selectedComponents = make(map[string][]uuid.UUID)
	for _, compSelector := range componentSelectors {
		h, _ := hash.JSONMD5Hash(compSelector)
		foundIDs, err := query.FindComponentIDs(ctx, -1, compSelector)
		if err != nil {
			return nil, err
		}

		selectedComponents[h] = foundIDs
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
	return utils.GetOrLoad(teamSpecCache, id, func() (*api.TeamSpec, error) {
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

		return &teamSpec, nil
	})
}

func PurgeCache(id string) {
	teamSpecCache.Delete(id)
}
