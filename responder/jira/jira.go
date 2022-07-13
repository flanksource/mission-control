package jira

import (
	"encoding/json"
	"fmt"
	"os"
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

// TODO: Move to utils to flanksource/commons
func requiredEnvVars(keys ...string) error {
	var unsetVars []string
	for _, key := range keys {
		_, exists := os.LookupEnv(key)
		if !exists {
			unsetVars = append(unsetVars, key)
		}
	}

	if len(unsetVars) > 0 {
		return fmt.Errorf("Required environment variables: %s", unsetVars)
	}

	return nil
}

func getClient() (*jira.Client, error) {

	err := requiredEnvVars("JIRA_EMAIL", "JIRA_API_TOKEN", "JIRA_URL")
	if err != nil {
		return nil, err
	}

	tp := jira.BasicAuthTransport{
		Username: os.Getenv("JIRA_EMAIL"),
		Password: os.Getenv("JIRA_API_TOKEN"),
	}

	//client, err := jira.NewClient(tp.Client(), "https://flanksource.atlassian.net")
	client, err := jira.NewClient(tp.Client(), os.Getenv("JIRA_URL"))
	if err != nil {
		return nil, err
	}
	return client, nil

}

func CreateIssue(opts JiraOptions) error {

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

	client, err := getClient()
	if err != nil {
		return err
	}
	issue, _, err := client.Issue.Create(&i)
	if err != nil {
		return err
	}
	logger.Debugf("[Jira] Issue created with ID: [%s]", issue.ID)

	return nil
}

func AddComment(issueID, comment string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	commObj := &jira.Comment{Body: comment}
	c, _, err := client.Issue.AddComment(issueID, commObj)
	if err != nil {
		return err
	}

	logger.Debugf("[Jira] Comment: [%s] added for issueID: %s", c.Body, issueID)

	return nil

}

func GetComments(issueID string) ([]api.Comment, error) {

	client, err := getClient()
	if err != nil {
		return nil, err
	}

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

func GetConfig() (json.RawMessage, error) {

	client, err := getClient()
	if err != nil {
		return nil, err
	}

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
