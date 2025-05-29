package api

import (
	"time"

	"github.com/flanksource/duty/types"
)

// Application is the schema that UI uses.
type Application struct {
	ApplicationDetail `json:",inline"`
	AccessControl     ApplicationAccessControl   `json:"accessControl"`
	Changes           []ApplicationChange        `json:"changes"`
	Incidents         []ApplicationIncident      `json:"incidents"`
	Locations         []ApplicationLocation      `json:"locations"`
	Backups           []ApplicationBackup        `json:"backups"`
	Restores          []ApplicationBackupRestore `json:"restores"`
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
	// Environment ID like an AWS account ID or Azure subscription ID
	ID string `json:"id"`

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
}

type ApplicationChange struct {
	ID          string    `json:"id"`
	Date        time.Time `json:"date"`
	User        string    `json:"user"`
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
