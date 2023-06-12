package api

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

type Team struct {
	ID        uuid.UUID     `json:"id" gorm:"default:generate_ulid()"`
	Name      string        `json:"name"`
	Spec      types.JSONMap `json:"properties" gorm:"type:jsonstringmap;<-:false"`
	DeletedAt *time.Time    `json:"deleted_at"`
}

func (t Team) HasResponder() bool {
	teamSpec, err := t.GetSpec()
	if err != nil {
		return false
	}
	return !teamSpec.ResponderClients.IsEmpty()
}

func (t Team) GetSpec() (TeamSpec, error) {
	var teamSpec TeamSpec
	teamSpecJson, err := t.Spec.MarshalJSON()
	if err != nil {
		return teamSpec, err
	}
	if err = json.Unmarshal(teamSpecJson, &teamSpec); err != nil {
		return teamSpec, err
	}
	return teamSpec, nil
}

type TeamSpec struct {
	Components       []ComponentSelector  `json:"components,omitempty"`
	ResponderClients ResponderClients     `json:"responder_clients"`
	Notifications    []NotificationConfig `json:"notifications,omitempty"`
}

type Person struct {
	ID         uuid.UUID        `json:"id" gorm:"default:generate_ulid()"`
	Name       string           `json:"name,omitempty"`
	Email      string           `json:"email,omitempty"`
	Avatar     string           `json:"avatar,omitempty"`
	Properties PersonProperties `json:"properties,omitempty"`
}

func (person Person) TableName() string {
	return "people"
}

type PersonProperties struct {
	Role string `json:"role,omitempty"`
}

func (p PersonProperties) Value() (driver.Value, error) {
	return types.GenericStructValue(p, true)
}

func (p *PersonProperties) Scan(val any) error {
	return types.GenericStructScan(&p, val)
}

type TeamComponent struct {
	TeamID      uuid.UUID `json:"team_id"`
	ComponentID uuid.UUID `json:"component_id"`
	SelectorID  string    `json:"selector_id,omitempty"`
	Role        string    `json:"role,omitempty"`
}
