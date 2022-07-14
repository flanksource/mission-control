package jira

import (
	"encoding/json"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"

	jira "github.com/andygrunwald/go-jira"
)

type JiraOptions struct {
	ProjectKey  string
	Summary     string
	Description string
	IssueType   string
}

func GetClient(email, apiToken, url string) (*jira.Client, error) {

	tp := jira.BasicAuthTransport{
		Username: email,
		Password: apiToken,
	}

	client, err := jira.NewClient(tp.Client(), url)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func CreateIssue(client *jira.Client, opts JiraOptions) error {

	i := jira.Issue{
		Fields: &jira.IssueFields{
			//Reporter: &jira.User{
			//Name: opts.Reporter,
			//},
			Description: opts.Description,
			Type: jira.IssueType{
				Name: opts.IssueType,
			},
			Project: jira.Project{
				Key: opts.ProjectKey,
			},
			Summary: opts.Summary,
		},
	}

	issue, _, err := client.Issue.Create(&i)
	if err != nil {
		return err
	}
	logger.Debugf("[Jira] Issue created with ID: [%s]", issue.ID)

	return nil
}

func AddComment(client *jira.Client, issueID, comment string) error {

	commObj := &jira.Comment{Body: comment}
	c, _, err := client.Issue.AddComment(issueID, commObj)
	if err != nil {
		return err
	}

	logger.Debugf("[Jira] Comment: [%s] added for issueID: %s", c.Body, issueID)

	return nil
}

func GetComments(client *jira.Client, issueID string) ([]api.Comment, error) {

	issue, _, err := client.Issue.Get(issueID, nil)
	if err != nil {
		return nil, err
	}

	var comments []api.Comment
	for _, comment := range issue.Fields.Comments.Comments {
		createdAt, _ := time.Parse("2006-01-02T15:04:05.999-0700", comment.Created)
		comments = append(comments, api.Comment{
			Body:      comment.Body,
			CreatedBy: comment.Author.DisplayName,
			CreatedAt: createdAt,
		})
	}

	return comments, nil
}

func GetConfig(client *jira.Client) (json.RawMessage, error) {

	projects, _, err := client.Project.GetList()
	if err != nil {
		return nil, err
	}

	p := make(map[string]interface{})
	for _, projectMeta := range *projects {
		project, _, err := client.Project.Get(projectMeta.ID)
		if err != nil {
			return nil, err
		}
		p[projectMeta.Name] = project
	}

	// TODO: Use priority service to get priorities
	projectsJson, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}

	return projectsJson, nil
}

func CloseIssue(issueId string) error {
	// Update issue state
	// Issue state can be self defined
	// Use `Done` for now since that is the default
	return nil
}
