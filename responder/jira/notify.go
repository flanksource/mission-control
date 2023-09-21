package jira

import (
	"fmt"

	goJira "github.com/andygrunwald/go-jira"
	"github.com/flanksource/incident-commander/api"
	"github.com/mitchellh/mapstructure"
)

func (jc *JiraClient) NotifyResponder(ctx api.Context, responder api.Responder) (string, error) {
	if responder.Properties["responderType"] != ResponderType {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	var issueOptions JiraIssue
	err := mapstructure.Decode(responder.Properties, &issueOptions)
	if err != nil {
		return "", err
	}

	var issue *goJira.Issue
	if issue, err = jc.CreateIssue(issueOptions); err != nil {
		return "", err
	}

	return issue.Key, nil
}

func (jc *JiraClient) NotifyResponderAddComment(ctx api.Context, responder api.Responder, comment string) (string, error) {
	if responder.Properties["responderType"] != ResponderType {
		return "", fmt.Errorf("invalid responderType: %s", responder.Properties["responderType"])
	}

	commentId, err := jc.AddComment(responder.ExternalID, comment)
	if err != nil {
		return "", err
	}

	return commentId, nil
}
