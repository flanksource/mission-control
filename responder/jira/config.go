package jira

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/pkg/errors"
)

func (jc *JiraClient) SyncConfig(ctx *api.Context, team api.Team) (configType string, configName string, config string, err error) {
	config, err = jc.GetConfigJSON()
	if err != nil {
		return "", "", "", errors.Wrap(err, "error generating config from Jira")
	}

	teamSpec, err := team.GetSpec()
	if err != nil {
		return "", "", "", errors.Wrap(err, "error getting team spec")
	}
	configName = teamSpec.ResponderClients.Jira.Values["project"]
	configType = ResponderType
	return
}
