package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IncidentType string

var (
	IncidentTypeAvailability  IncidentType = "availability"
	IncidentTypeCost          IncidentType = "cost"
	IncidentTypePerformance   IncidentType = "performance"
	IncidentTypeSecurity      IncidentType = "security"
	IncidentTypeTechnicalDebt IncidentType = "technical_debt"
	IncidentTypeCompliance    IncidentType = "compliance"
	IncidentTypeIntegration   IncidentType = "integration"
)

type IncidentStatus string

var (
	IncidentStatusOpen      IncidentStatus = "open"
	IncidentStatusClosed    IncidentStatus = "closed"
	IncidentStatusMitigated IncidentStatus = "mitigated"
	IncidentStatusResolved  IncidentStatus = "resolved"
	IncidentStatusCancelled IncidentStatus = "cancelled"
)

type Incident struct {
	ID             *uuid.UUID     `json:"id" gorm:"default:generate_ulid(),omitempty"`
	Title          string         `json:"title,omitempty"`
	Description    string         `json:"description,omitempty"`
	Type           IncidentType   `json:"type,omitempty"`
	Status         IncidentStatus `json:"status,omitempty"`
	Severity       string         `json:"severity,omitempty"`
	CreatedAt      *time.Time     `json:"created_at,omitempty"`
	UpdatedAt      *time.Time     `json:"updated_at,omitempty"`
	Acknowledged   *time.Time     `json:"acknowledged,omitempty"`
	Resolved       *time.Time     `json:"resolved,omitempty"`
	Closed         *time.Time     `json:"closed,omitempty"`
	CreatedBy      *uuid.UUID     `json:"created_by,omitempty"`
	IncidentRuleID *uuid.UUID     `json:"incident_rule_id,omitempty"`
	CommanderID    *uuid.UUID     `json:"commander_id,omitempty"`
	CommunicatorID *uuid.UUID     `json:"communicator_id,omitempty"`
}

func (i *Incident) BeforeCreate(tx *gorm.DB) (err error) {
	if i.CreatedBy == nil {
		i.CreatedBy = SystemUserID
	}
	return
}

func (hypothesis *Hypothesis) BeforeCreate(tx *gorm.DB) (err error) {
	if hypothesis.CreatedBy == nil {
		hypothesis.CreatedBy = SystemUserID
	}
	return
}

func (evidence *Evidence) BeforeCreate(tx *gorm.DB) (err error) {
	if evidence.CreatedBy == uuid.Nil {
		evidence.CreatedBy = *SystemUserID
	}
	if evidence.ID == uuid.Nil {
		evidence.ID = uuid.New()
	}
	return
}

func (incident Incident) Clone() Incident {
	clone := incident
	return clone
}

type IncidentHistory struct {
	IncidentID   uuid.UUID  `json:"incident_id,omitempty"`
	Type         string     `json:"type,omitempty"`
	Description  string     `json:"description,omitempty"`
	HypothesisID *uuid.UUID `json:"hypothesis_id,omitempty"`
	CreatedBy    *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

func (ih IncidentHistory) TableName() string {
	return "incident_histories"

}

type Comment struct {
	ID                uuid.UUID  `json:"id" gorm:"default:generate_ulid(),omitempty"`
	IncidentID        uuid.UUID  `json:"incident_id,omitempty"`
	ExternalID        string     `json:"external_id,omitempty"`
	Comment           string     `json:"comment,omitempty"`
	CreatedBy         uuid.UUID  `json:"created_by,omitempty"`
	ExternalCreatedBy string     `json:"external_created_by,omitempty"`
	CreatedAt         time.Time  `json:"created_at,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	ResponderID       *uuid.UUID `json:"responder_id,omitempty"`
	HypothesisID      *uuid.UUID `json:"hypothesis_id,omitempty"`
	Incident          Incident   `json:"incident,omitempty"`
}

// Gorm entity for the hpothesis table
type Hypothesis struct {
	ID         uuid.UUID  `json:"id" gorm:"default:generate_ulid(),omitempty"`
	IncidentID uuid.UUID  `json:"incident_id,omitempty"`
	Type       string     `json:"type,omitempty"`
	Title      string     `json:"title,omitempty"`
	Status     string     `json:"status,omitempty"`
	ParentID   *uuid.UUID `json:"parent_id,omitempty"`
	TeamID     *uuid.UUID `json:"team_id,omitempty"`
	Owner      *uuid.UUID `json:"owner,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
}

func (Hypothesis) TableName() string {
	return "hypotheses"
}

type Filter struct {
	Status []string `json:"status,omitempty"`
	// Only match incidents with the given status, use * to match all
	Severity []string `json:"severity,omitempty"`
	// Only match incidents with the given category (cost,performance,security,availability), use * to match all
	Category []string `json:"category,omitempty"`
	// How long the health check must be failing for, before opening an incident
	Age *time.Duration `json:"age,omitempty"`
}

func (f Filter) String() string {
	s := ""
	if len(f.Status) > 0 {
		s += "status=" + strings.Join(f.Status, ",")
	}
	if len(f.Severity) > 0 {
		s += " severity=" + strings.Join(f.Severity, ",")
	}
	if len(f.Category) > 0 {
		s += " category=" + strings.Join(f.Category, ",")
	}
	if f.Age != nil {
		s += " age=" + f.Age.String()
	}
	return strings.TrimSpace(s)
}

type AutoClose struct {
	// How long after the health checks have been passing before, autoclosing the incident.
	Timeout time.Duration `json:"timeout,omitempty"`
}

type HoursOfOperation struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Negate bool
}

type IncidentRuleSpec struct {
	Name            string              `json:"name,omitempty"`
	Components      []ComponentSelector `json:"components,omitempty"`
	Template        Incident            `json:"template,omitempty"`
	Filter          Filter              `json:"filter,omitempty"`
	AutoAssignOwner bool                `json:"autoAssignOwner,omitempty"`
	// order of processing rules
	Priority int `json:"priority,omitempty"`
	// stop processing other incident rules, when matched
	BreakOnMatch       bool               `json:"breakOnMatch,omitempty"`
	HoursOfOperation   []HoursOfOperation `json:"hoursOfOperation,omitempty"`
	AutoClose          *AutoClose         `json:"autoClose,omitempty"`
	AutoResolve        *AutoClose         `json:"autoResolve,omitempty"`
	IncidentResponders IncidentResponders `json:"responders,omitempty"`
}

func (rule IncidentRuleSpec) String() string {
	return fmt.Sprintf("name=%s components=%s filter=%s", rule.Name, rule.Components, rule.Filter)
}
