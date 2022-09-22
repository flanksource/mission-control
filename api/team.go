package api

import (
	"encoding/json"

	"github.com/flanksource/incident-commander/db/types"
	"github.com/google/uuid"
)

type Team struct {
	ID   uuid.UUID     `json:"id" gorm:"default:generate_ulid()"`
	Name string        `json:"name"`
	Spec types.JSONMap `json:"properties" gorm:"type:jsonstringmap;<-:false"`
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
	Components       []ComponentSelector `json:"components,omitempty"`
	ResponderClients ResponderClients    `json:"responder_clients"`
}

type Person struct {
	ID     uuid.UUID `json:"id" gorm:"default:generate_ulid()"`
	Name   string    `json:"name,omitempty"`
	Email  string    `json:"email,omitempty"`
	Avatar string    `json:"avatar,omitempty"`
	Role   string    `json:"role,omitempty"`
}

func (person Person) TableName() string {
	return "people"
}

type TeamComponent struct {
	TeamID      uuid.UUID `json:"team_id"`
	ComponentID uuid.UUID `json:"component_id"`
	SelectorID  string    `json:"selector_id,omitempty"`
	Role        string    `json:"role,omitempty"`
}
