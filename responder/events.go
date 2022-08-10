package responder

import (
	"encoding/json"
	"fmt"

	goJira "github.com/andygrunwald/go-jira"
	msgraphModels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/mitchellh/mapstructure"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder/jira"
	"github.com/flanksource/incident-commander/responder/msplanner"
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

func NotifyMSPlannerResponder(responder api.Responder) (string, error) {
	if responder.Properties["responderType"] != "MSPlanner" {
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

	client, err := msplanner.NewClient(
		teamSpec.ResponderClients.MSPlanner.TenantID,
		teamSpec.ResponderClients.MSPlanner.ClientID,
		teamSpec.ResponderClients.MSPlanner.GroupID,
		teamSpec.ResponderClients.MSPlanner.Username.Value,
		teamSpec.ResponderClients.MSPlanner.Password.Value,
	)
	if err != nil {
		return "", err
	}

	var taskOptions msplanner.MSPlannerTask
	err = mapstructure.Decode(responder.Properties, &taskOptions)
	if err != nil {
		return "", err
	}

	var task msgraphModels.PlannerTaskable
	if task, err = client.CreateTask(taskOptions); err != nil {
		return "", err
	}

	return *task.GetId(), nil
}
