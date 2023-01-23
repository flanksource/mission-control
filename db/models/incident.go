package models

import (
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Gorm entity for the hpothesis table
type IncidentRule struct {
	ID     *uuid.UUID `json:"id,omitempty" gorm:"default:generate_ulid()"`
	Name   string     `json:"name,omitempty"`
	Source string     `json:"source,omitempty"`
	// TODO make this a custom GORM type
	Spec      types.JSON            `json:"spec,omitempty"`
	CreatedAt *time.Time            `json:"created_at,omitempty"`
	UpdatedAt *time.Time            `json:"updated_at,omitempty"`
	CreatedBy *uuid.UUID            `json:"created_by,omitempty"`
	spec      *api.IncidentRuleSpec `json:"-" gorm:"-"`
}

func (incidentRule *IncidentRule) GetSpec() (*api.IncidentRuleSpec, error) {
	if incidentRule.spec != nil {
		return incidentRule.spec, nil
	}
	incidentRule.spec = &api.IncidentRuleSpec{}
	if err := jsoniter.Unmarshal(incidentRule.Spec, incidentRule.spec); err != nil {
		return nil, err
	}
	return incidentRule.spec, nil
}

func (incidentRule *IncidentRule) BeforeCreate(tx *gorm.DB) (err error) {
	if incidentRule.CreatedBy == nil {
		incidentRule.CreatedBy = api.SystemUserID
	}
	return
}
