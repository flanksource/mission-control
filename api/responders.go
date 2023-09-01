package api

import (
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// +kubebuilder:object:generate=true
type IncidentResponders struct {
	Email       []Email         `json:"email,omitempty"`
	Jira        []Jira          `json:"jira,omitempty"`
	AWS         []CloudProvider `json:"aws,omitempty"`
	AMS         []CloudProvider `json:"ams,omitempty"`
	GCP         []CloudProvider `json:"gcp,omitempty"`
	ServiceNow  []ServiceNow    `json:"servicenow,omitempty"`
	Slack       []Slack         `json:"slack,omitempty"`
	Teams       []TeamsChannel  `json:"teams,omitempty"`
	TeamsUser   []TeamsUser     `json:"teamsUser,omitempty"`
	GithubIssue []GithubIssue   `json:"github,omitempty"`
}

type Responder struct {
	ID         uuid.UUID           `json:"id,omitempty"`
	Type       string              `json:"type"`
	Properties types.JSONStringMap `json:"properties" gorm:"type:jsonstringmap;<-:false"`
	ExternalID string              `json:"external_id,omitempty"`
	IncidentID uuid.UUID           `json:"incident_id,omitempty"`
	Incident   Incident            `json:"incident,omitempty"`
	TeamID     uuid.UUID           `json:"team_id,omitempty"`
	Team       Team                `json:"team,omitempty"`
}

type NotificationSpec struct {
	Icon  string `json:"icon,omitempty"`
	Emoji string `json:"emoji,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
}

type TeamsUser struct {
	NotificationSpec `json:",inline"`
}

type TeamsChannel struct {
}

type Slack struct {
	NotificationSpec `json:",inline"`
	Channel          string `json:"channel"`
}

type ResponderClients struct {
	Jira      *JiraClient      `json:"jira,omitempty"`
	AWS       *AWSClient       `json:"aws,omitempty"`
	MSPlanner *MSPlannerClient `json:"ms_planner,omitempty"`
}

func (r ResponderClients) IsEmpty() bool {
	return r.Jira == nil && r.AWS == nil && r.MSPlanner == nil
}

type ServiceNow struct {
	Project     string `json:"project,omitempty"`
	IssueType   string `json:"issueType,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Description string `json:"description,omitempty"`
}

type AWSClient struct {
	AccessKey types.EnvVar `yaml:"username" json:"username,omitempty"`
	SecretKey types.EnvVar `yaml:"password" json:"password,omitempty"`
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

type ResponderClientBase struct {
	Defaults map[string]string `json:"defaults"`
	Values   map[string]string `json:"values"`
}

type JiraClient struct {
	ResponderClientBase `json:",inline"`
	Url                 string       `json:"url,omitempty"`
	Username            types.EnvVar `yaml:"username" json:"username"`
	Password            types.EnvVar `yaml:"password" json:"password"`
}

type MSPlannerClient struct {
	ResponderClientBase `json:",inline"`
	TenantID            string       `json:"tenant_id"`
	ClientID            string       `json:"client_id"`
	GroupID             string       `json:"group_id"`
	Username            types.EnvVar `yaml:"username" json:"username"`
	Password            types.EnvVar `yaml:"password" json:"password"`
}

type Jira struct {
	Project     string `json:"project,omitempty"`
	Summary     string `json:"summary"`
	IssueType   string `json:"issueType,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	Description string `json:"description,omitempty"`
}

// +kubebuilder:object:generate=true
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
