//nolint:staticcheck
package responder

import (
	"fmt"

	goJira "github.com/andygrunwald/go-jira"
	msgraphModels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/mitchellh/mapstructure"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder/jira"
	"github.com/flanksource/incident-commander/responder/msplanner"
)

const (
	JiraResponder      = "Jira"
	MSPlannerResponder = "MSPlanner"
)

func NotifyJiraResponder(responder api.Responder) (string, error) {
	if responder.Properties["responderType"] != JiraResponder {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	teamSpec, err := responder.Team.GetSpec()
	if err != nil {
		return "", err
	}
	client, err := jiraClientFromTeamSpec(teamSpec)
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

func jiraClientFromTeamSpec(teamSpec api.TeamSpec) (jira.JiraClient, error) {
	return jira.NewClient(
		teamSpec.ResponderClients.Jira.Username.Value,
		teamSpec.ResponderClients.Jira.Password.Value,
		teamSpec.ResponderClients.Jira.Url,
	)
}

func msPlannerClientFromTeamSpec(teamSpec api.TeamSpec) (msplanner.MSPlannerClient, error) {
	return msplanner.NewClient(
		teamSpec.ResponderClients.MSPlanner.TenantID,
		teamSpec.ResponderClients.MSPlanner.ClientID,
		teamSpec.ResponderClients.MSPlanner.GroupID,
		teamSpec.ResponderClients.MSPlanner.Username.Value,
		teamSpec.ResponderClients.MSPlanner.Password.Value,
	)
}

func NotifyMSPlannerResponder(responder api.Responder) (string, error) {
	if responder.Properties["responderType"] != MSPlannerResponder {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	teamSpec, err := responder.Team.GetSpec()
	if err != nil {
		return "", err
	}
	client, err := msPlannerClientFromTeamSpec(teamSpec)
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

func NotifyJiraResponderAddComment(responder api.Responder, comment string) (string, error) {
	if responder.Properties["responderType"] != JiraResponder {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	teamSpec, err := responder.Team.GetSpec()
	if err != nil {
		return "", err
	}
	client, err := jiraClientFromTeamSpec(teamSpec)
	if err != nil {
		return "", err
	}

	commentObj, err := client.AddComment(responder.ExternalID, comment)
	if err != nil {
		return "", err
	}

	return commentObj.ID, nil
}

func NotifyMSPlannerResponderAddComment(responder api.Responder, comment string) (string, error) {
	if responder.Properties["responderType"] != MSPlannerResponder {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	teamSpec, err := responder.Team.GetSpec()
	if err != nil {
		return "", err
	}
	client, err := msPlannerClientFromTeamSpec(teamSpec)
	if err != nil {
		return "", err
	}
	return client.AddComment(responder.ExternalID, comment)
}
