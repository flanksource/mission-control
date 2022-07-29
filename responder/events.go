package responder

import (
	"encoding/json"
	"fmt"

	goJira "github.com/andygrunwald/go-jira"
	"github.com/mitchellh/mapstructure"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder/jira"
)

func NotifyJiraResponder(responder api.Responder) (string, error) {
	if responder.Properties["responderType"] != "Jira" {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	teamSpecJson, err := responder.Team.Spec.MarshalJSON()
	if err != nil {
		return "", err
	}

	var teamSpec api.TeamSpec
	if err = json.Unmarshal(teamSpecJson, &teamSpec); err != nil {
		return "", err
	}

	client, err := jira.NewClient(
		teamSpec.ResponderClients.Jira.Username.Value,
		teamSpec.ResponderClients.Jira.Password.Value,
		teamSpec.ResponderClients.Jira.Url,
	)
	if err != nil {
		return "", err
	}

	var issueOptions jira.JiraIssue
	err = mapstructure.Decode(responder.Properties, &issueOptions)
	if err != nil {
		return "", err
	}

	var issue *goJira.Issue
	if issue, err = client.CreateIssue(issueOptions); err != nil {
		return "", err
	}

	return issue.Key, nil
}
