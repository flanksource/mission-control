package responder

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	_ "github.com/flanksource/kommons"
	"github.com/mitchellh/mapstructure"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/responder/jira"
)

func ReconcileEvents(events []api.Event) {

	var responderIDs []string
	responderIDEventMap := make(map[string]api.Event)
	for _, event := range events {
		responderIDs = append(responderIDs, event.Properties["id"])
		responderIDEventMap[event.Properties["id"]] = event
	}

	var responders []api.Responder

	tx := db.Gorm.Find(&responders).Where("id IN (?) AND external_id is NULL", responderIDs).Preload("Team")
	if tx.Error != nil {
		logger.Errorf("Error while fetching responders from database: %v", tx.Error)
		return
	}

	var externalID string
	var err error
	for _, responder := range responders {
		event := responderIDEventMap[responder.ID.String()]
		if responder.Properties["responderType"] == "Jira" {
			fmt.Println("Inside responder properties jira")
			externalID, err = NotifyJiraResponder(responder)
			if err != nil {
				setErr := event.SetErrorMessage(err.Error())
				if setErr != nil {
					logger.Errorf("Error updating table:event_queue with id:%s and error:%s", event.ID, err)
				}
				continue
			}
		}

		if externalID != "" {
			// Update external id in responder table
			tx := db.Gorm.Model(&api.Responder{}).Where("id = ?", responder.ID).Update("external_id", externalID)
			if tx.Error != nil {
				logger.Errorf("Error updating table:responder with id:%s and external_id:%s", responder.ID, externalID)
			}

			// Cautionary assignment to prevent previous external_id being set in database
			externalID = ""
		}

		event.Done()
	}
}

func NotifyJiraResponder(responder api.Responder) (string, error) {
	if responder.Properties["responderType"] != "Jira" {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	// TODO: This would be used when team based workflow would kick in
	teamSpec := responder.Team.Spec

	// TODO: Temp solution to test workflow
	// Get team details of all the responders
	// team spec has jira details
	/*
		teamSpec := api.TeamSpec{
			ResponderClients: api.ResponderClients{
				Jira: api.JiraClient{
					Url:      "https://flanksource.atlassian.net",
					Username: kommons.EnvVar{Value: "yash@flanksource.com"},
					Password: kommons.EnvVar{Value: "7NCOe92hvnKZBzjenSOnD673"},
				},
			},
		}*/

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

	issue, err := client.CreateIssue(issueOptions)

	if err != nil {
		return "", err
	}
	return issue.Key, nil
}
