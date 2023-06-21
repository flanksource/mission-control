package msplanner

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/pkg/errors"
)

func (client *MSPlannerClient) SyncConfig(ctx *api.Context, team api.Team) (configType string, configName string, config string, err error) {
	config, err = client.GetConfigJSON()
	if err != nil {
		return "", "", "", errors.Wrap(err, "error generating config from MSPlanner")
	}
	teamSpec, err := team.GetSpec()
	if err != nil {
		return "", "", "", errors.Wrap(err, "error getting team spec")
	}

	configType = ResponderType
	configName = teamSpec.ResponderClients.MSPlanner.Values["plan"]
	return
}
