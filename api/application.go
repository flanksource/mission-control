package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/view"
)

const (
	SectionTypeView    = "view"
	SectionTypeChanges = "changes"
	SectionTypeConfigs = "configs"
)

// ApplicationSection is a typed section in an application response.
// The Type field is one of "view", "changes", or "configs".
// Only the field matching the type is populated.
type ApplicationSection struct {
	Type    string                  `json:"type"`
	Title   string                  `json:"title"`
	Icon    string                  `json:"icon,omitempty"`
	View    *ApplicationViewData    `json:"view,omitempty"`
	Changes []ApplicationChange     `json:"changes,omitempty"`
	Configs []ApplicationConfigItem `json:"configs,omitempty"`
}

// ApplicationViewData holds the data-only fields from a resolved ViewRef section.
// UI-only metadata (ResponseSource, RefreshError, RequestFingerprint, etc.) is excluded.
type ApplicationViewData struct {
	RefreshStatus   string                         `json:"refreshStatus,omitempty"`
	LastRefreshedAt *time.Time                     `json:"lastRefreshedAt,omitempty"`
	Columns         []view.ColumnDef               `json:"columns,omitempty"`
	Rows            []view.Row                     `json:"rows,omitempty"`
	Panels          []PanelResult                  `json:"panels,omitempty"`
	Variables       []ViewVariableWithOptions       `json:"variables,omitempty"`
	ColumnOptions   map[string]ColumnFilterOptions `json:"columnOptions,omitempty"`
}

// ApplicationConfigItem is a typed config item returned in a configs section.
type ApplicationConfigItem struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Type   string            `json:"type,omitempty"`
	Status string            `json:"status,omitempty"`
	Health string            `json:"health,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Application is the schema that UI uses.
type Application struct {
	ApplicationDetail `json:",inline"`
	AccessControl     ApplicationAccessControl   `json:"accessControl"`
	Incidents         []ApplicationIncident      `json:"incidents"`
	Locations         []ApplicationLocation      `json:"locations"`
	Backups           []ApplicationBackup        `json:"backups"`
	Restores          []ApplicationBackupRestore `json:"restores"`
	Findings          []ApplicationFinding       `json:"findings"`
	Sections          []ApplicationSection       `json:"sections"`
}

type ApplicationFinding struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Severity     string    `json:"severity"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Date         time.Time `json:"date"`
	LastObserved time.Time `json:"lastObserved"`
	Status       string    `json:"status"`
}

type ApplicationBackup struct {
	ID       string    `json:"id"`
	Database string    `json:"database"`
	Type     string    `json:"type"`
	Source   string    `json:"source"`
	Date     time.Time `json:"date"`
	Size     string    `json:"size"`
	Status   string    `json:"status"`
}

type ApplicationBackupRestore struct {
	ID          string    `json:"id"`
	Database    string    `json:"database"`
	Date        time.Time `json:"date"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completedAt"`
}

type ApplicationLocation struct {
	// Environment Account like an AWS account Account or Azure subscription Account
	Account string `json:"account"`

	// Name of the environment
	Name string `json:"name"`

	// Type of the environment. Example: cloud, on-prem, etc.
	Type string `json:"type"`

	// Purpose of the environment (e.g., "primary", "backup").
	Purpose string `json:"purpose"`

	// Region or the location
	Region string `json:"region"`

	// Provider of the location. Example: AWS, Azure, etc.
	Provider string `json:"provider"`

	// Total number of resources in the environment
	ResourceCount int `json:"resourceCount"`
}

type ApplicationChange struct {
	ID          string    `json:"id"`
	Date        time.Time `json:"date"`
	ChangeType  string    `json:"changeType,omitempty"`
	Source      string    `json:"source,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ApplicationAccessControl struct {
	Users          []UserAndRole `json:"users"`
	Authentication []AuthMethod  `json:"authentication"`
}

type ApplicationDetail struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Namespace   string     `json:"namespace"`
	Description string     `json:"description,omitempty"`
	Properties  []Property `json:"properties,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type UserAndRole struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	AuthType         string     `json:"authType"`
	CreatedAt        time.Time  `json:"created"`
	LastLogin        *time.Time `json:"lastLogin,omitempty"`
	LastAccessReview *time.Time `json:"lastAccessReview,omitempty"`
}

type AuthMethod struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	MFA        *AuthMethodMFA    `json:"mfa,omitempty"`
	Properties map[string]string `json:"properties"`
}

type AuthMethodMFA struct {
	Type     string `json:"type"`
	Enforced string `json:"enforced"`
}

type ApplicationIncident struct {
	ID           string     `json:"id"`
	Date         time.Time  `json:"date"`
	Severity     string     `json:"severity"`
	Description  string     `json:"description"`
	Status       string     `json:"status"`
	ResolvedDate *time.Time `json:"resolvedDate,omitempty"`
}

// +kubebuilder:object:generate=true
type Property struct {
	Label   string       `json:"label,omitempty"`
	Name    string       `json:"name,omitempty"`
	Tooltip string       `json:"tooltip,omitempty"`
	Icon    string       `json:"icon,omitempty"`
	Text    string       `json:"text,omitempty"`
	Order   int          `json:"order,omitempty"`
	Type    string       `json:"type,omitempty"`
	Color   string       `json:"color,omitempty"`
	Value   *int64       `json:"value,omitempty"`
	Links   []types.Link `json:"links,omitempty"`

	// e.g. milliseconds, bytes, millicores, epoch etc.
	Unit string `json:"unit,omitempty"`
}
