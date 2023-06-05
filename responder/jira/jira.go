package jira

import (
	"encoding/json"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"

	jira "github.com/andygrunwald/go-jira"
)

const ResponderType = "Jira"

type JiraIssue struct {
	Project     string
	Summary     string
	Description string
	IssueType   string
	Priority    string
}

type JiraProject struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	IssueTypes []string `json:"issueTypes"`
	Priorities []string `json:"priorities"`
	Statuses   []string `json:"statuses"`
}

type JiraIssueTransitions struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ToState string `json:"to_state"`
}

type JiraConfig struct {
	Projects map[string]JiraProject `json:"projects"`
}

type JiraClient struct {
	client *jira.Client
}

// Jira does not support creating an issue of
// type Sub-task so we do not set it in config
var IssueTypeExcludeList = []string{"Sub-task"}

func NewClient(ctx *api.Context, team api.Team) (*JiraClient, error) {
	teamSpec, err := team.GetSpec()
	if err != nil {
		return nil, err
	}
	username, err := ctx.GetEnvVarValue(teamSpec.ResponderClients.Jira.Username)
	if err != nil {
		return nil, err
	}
	password, err := ctx.GetEnvVarValue(teamSpec.ResponderClients.Jira.Password)
	if err != nil {
		return nil, err
	}

	return newClient(username, password, teamSpec.ResponderClients.Jira.Url)
}

func newClient(email, apiToken, url string) (*JiraClient, error) {
	tr := jira.BasicAuthTransport{
		Username: email,
		Password: apiToken,
	}

	c, err := jira.NewClient(tr.Client(), url)
	if err != nil {
		return nil, err
	}

	client := &JiraClient{client: c}
	return client, nil
}

func (jc *JiraClient) CreateIssue(opts JiraIssue) (*jira.Issue, error) {

	i := jira.Issue{
		Fields: &jira.IssueFields{
			Description: opts.Description,
			Type: jira.IssueType{
				Name: opts.IssueType,
			},
			Project: jira.Project{
				Key: opts.Project,
			},
			Summary: opts.Summary,
		},
	}

	if opts.Priority != "" {
		i.Fields.Priority = &jira.Priority{Name: opts.Priority}
	}

	issue, _, err := jc.client.Issue.Create(&i)
	if err != nil {
		return nil, err
	}
	logger.Debugf("[Jira] Issue created for Project: [%s] with ID: [%s] - [%s]", opts.Project, issue.Key, opts.Summary)
	return issue, nil
}

func (jc *JiraClient) AddComment(issueID, comment string) (string, error) {
	c, _, err := jc.client.Issue.AddComment(issueID, &jira.Comment{Body: comment})
	if err != nil {
		return "", err
	}

	logger.Debugf("[Jira] Comment: [%s] added for issueID: [%s]", c.Body, issueID)
	return c.ID, nil
}

func (jc *JiraClient) GetComments(issueID string) ([]api.Comment, error) {
	issue, _, err := jc.client.Issue.Get(issueID, nil)
	if err != nil {
		return nil, err
	}

	var comments []api.Comment
	for _, comment := range issue.Fields.Comments.Comments {
		createdAt, _ := time.Parse("2006-01-02T15:04:05.999-0700", comment.Created)
		comments = append(comments, api.Comment{
			ExternalID:        comment.ID,
			Comment:           comment.Body,
			ExternalCreatedBy: comment.Author.DisplayName,
			CreatedAt:         createdAt,
		})
	}

	return comments, nil
}

func (jc JiraClient) GetConfig() (JiraConfig, error) {
	priorities, _, err := jc.client.Priority.GetList()
	if err != nil {
		return JiraConfig{}, err
	}

	var priorityList []string
	for _, priority := range priorities {
		priorityList = append(priorityList, priority.Name)
	}

	statuses, _, err := jc.client.Status.GetAllStatuses()
	if err != nil {
		return JiraConfig{}, err
	}
	var statusList []string
	for _, status := range statuses {
		statusList = append(statusList, status.Name)
	}

	projects, _, err := jc.client.Project.GetList()
	if err != nil {
		return JiraConfig{}, err
	}

	projectsMap := make(map[string]JiraProject)
	for _, projectMeta := range *projects {
		project, _, err := jc.client.Project.Get(projectMeta.ID)
		if err != nil {
			return JiraConfig{}, err
		}

		var issueTypes []string
		for _, issueType := range project.IssueTypes {
			if collections.Contains(IssueTypeExcludeList, issueType.Name) {
				continue
			}
			issueTypes = append(issueTypes, issueType.Name)
		}

		projectsMap[projectMeta.Name] = JiraProject{
			Key:        project.Key,
			Name:       project.Name,
			IssueTypes: issueTypes,
			Priorities: priorityList,
			Statuses:   statusList,
		}
	}

	return JiraConfig{Projects: projectsMap}, nil
}

func (jc JiraClient) GetConfigJSON() (string, error) {
	config, err := jc.GetConfig()
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(&config)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (jc JiraClient) GetIssueTransitions(issueID string) ([]JiraIssueTransitions, error) {
	transitions, _, err := jc.client.Issue.GetTransitions(issueID)
	if err != nil {
		return nil, err
	}

	var transitionList []JiraIssueTransitions
	for _, transition := range transitions {
		transitionList = append(transitionList, JiraIssueTransitions{
			ID:      transition.ID,
			Name:    transition.Name,
			ToState: transition.To.Name,
		})
	}
	return transitionList, nil
}

func (jc JiraClient) TransitionIssue(issueID, transitionID string) error {
	_, err := jc.client.Issue.DoTransition(issueID, transitionID)
	return err
}
