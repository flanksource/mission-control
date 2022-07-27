package api

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/kommons"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/db/types"
)

type Incident struct {
}

type Comment struct {
	Body      string    `json:"body"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

type Hypothesis struct {
}

type Responder struct {
	ID         uuid.UUID
	Properties types.JSONStringMap `json:"properties" gorm:"type:jsonstringmap;<-:false"`
	TeamID     uuid.UUID
	// TODO: Impement Scan+Value for kommons.EnvVar for gorm to work correctly
	//Team       Team
}

type Team struct {
	Spec TeamSpec
}

type Person struct {
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	Avatar string `json:"avatar,omitempty"`
	Role   string `json:"role,omitempty"`
}

type Notification struct {
	Icon  string `json:"icon"`
	Emoji string `json:"emoji"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type Slack struct {
	Notification `json:",inline"`
	Channel      string `json:"channel"`
}

type ResponderClients struct {
	Jira JiraClient
	AWS  AWSClient
}

type TeamSpec struct {
	ResponderClients ResponderClients
}

type TeamsUser struct {
	Notification `json:",inline"`
}

type TeamsChannel struct {
}

type IncidentResponders struct {
	Email       []Email         `json:"email"`
	Jira        []Jira          `json:"jira"`
	AWS         []CloudProvider `json:"aws"`
	AMS         []CloudProvider `json:"ams"`
	GCP         []CloudProvider `json:"gcp"`
	ServiceNow  []ServiceNow    `json:"servicenow"`
	Slack       []Slack         `json:"slack"`
	Teams       []TeamsChannel  `json:"teams"`
	TeamsUser   []TeamsUser     `json:"teamsUser"`
	GithubIssue []GithubIssue   `json:"github"`
}

type ServiceNow struct {
	Project     string `json:"project,omitempty"`
	IssueType   string `json:"issueType,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Description string `json:"description,omitempty"`
}

type AWSClient struct {
	AccessKey kommons.EnvVar `yaml:"username" json:"username"`
	SecretKey kommons.EnvVar `yaml:"password" json:"password"`
}

type AWSSupport struct {
	CloudProvider   `json:",inline"`
	ServiceCode     string `json:"serviceCode,omitempty"`
	CategoryCode    string `json:"categoryCode,omitempty"`
	Language        string `json:"language,omitempty"`
	CcEmailAddress  string `json:"ccEmailAddress,omitempty"`
	Body            string `json:"body,omitempty"`
	Subject         string `json:"subject,omitempty"`
	SeverityCode    string `json:"severityCode,omitempty"`
	AttachmentSetId string `json:"attachmentSetId,omitempty"`
}

type CloudProvider struct {
	Account     string `json:"account,omitempty"`
	Region      string `json:"region,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Description string `json:"description,omitempty"`
}

type GenericTicketing struct {
	Category    string            `json:"category,omitempty"`
	Description string            `json:"description,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type JiraClient struct {
	Url      string         `json:"url,omitempty"`
	Username kommons.EnvVar `yaml:"username" json:"username"`
	Password kommons.EnvVar `yaml:"password" json:"password"`
}

type Jira struct {
	Project     string `json:"project,omitempty"`
	Summary     string `json:"summary"`
	IssueType   string `json:"issueType,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Description string `json:"description,omitempty"`
}

type GithubIssue struct {
	Repository string   `json:"repository,omitempty"`
	Title      string   `json:"title,omitempty"`
	Body       string   `json:"body,omitempty"`
	Labels     []string `json:"labels,omitempty"`
}

type Email struct {
	To      string `json:"to,omitempty"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
}

type ComponentSelector struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Type      string            `json:"type,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type IncidentFilter struct {
	// Only match incidents with the given status, use * to match all
	Severity []string `json:"severity,omitempty"`
	// Only match incidents with the given category (cost,performance,security,availability), use * to match all
	Category []string `json:"category,omitempty"`
	// How long the health check must be failing for, before opening an incident
	Age time.Duration `json:"age,omitempty"`
}

type AutoClose struct {
	// How long after the health checks have been passing before, autoclosing the incident.
	Timeout time.Duration `json:"timeout,omitempty"`
}

type IncidentRule struct {
	Name               string              `json:"name,omitempty"`
	Components         []ComponentSelector `json:"components,omitempty"`
	Incident           IncidentFilter      `json:"filter,omitempty"`
	AutoAssignOwner    bool                `json:"autoAssignOwner,omitempty"`
	HoursOfOperation   string              `json:"hoursOfOperation,omitempty"`
	AutoClose          AutoClose           `json:"autoClose,omitempty"`
	AutoResolve        AutoClose           `json:"autoResolve,omitempty"`
	IncidentResponders IncidentResponders  `json:"responders,omitempty"`
}

type Event struct {
	ID         uuid.UUID
	Name       string
	Properties types.JSONStringMap `json:"properties" gorm:"type:jsonstringmap;<-:false"`
}

// We are using the term `Event` as it represents an event in the
// event_queue table, but the table is named event_queue
// to signify it's usage as a queue
func (Event) TableName() string {
	return "event_queue"
}

func (e Event) Done() {
	tx := db.Gorm.Delete(&e)
	if tx.Error != nil {
		logger.Errorf("Error deleting event from event_queue table with id:%s", e.ID.String())
	}
}

func (e Event) SetErrorMessage(errorMsg string) error {
	tx := db.Gorm.Exec("UPDATE event_queue SET error = ?, attempts = attempts + 1 WHERE id = ?", errorMsg, e.ID)
	return tx.Error
}
