package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Evidence struct {
	ID               uuid.UUID     `json:"id" gorm:"default:generate_ulid()"`
	HypothesisID     uuid.UUID     `json:"hypothesis_id"`
	ConfigID         *uuid.UUID    `json:"config_id"`
	ConfigChangeID   *uuid.UUID    `json:"config_change_id"`
	ConfigAnalysisID *uuid.UUID    `json:"config_analysis_id"`
	ComponentID      *uuid.UUID    `json:"component_id"`
	CheckID          *uuid.UUID    `json:"check_id"`
	Description      string        `json:"description"`
	DefinitionOfDone bool          `json:"definition_of_done"`
	Done             bool          `json:"done"`
	Factor           bool          `json:"factor"`
	Mitigator        bool          `json:"mitigator"`
	CreatedBy        uuid.UUID     `json:"created_by"`
	Type             string        `json:"type"`
	Script           string        `json:"script"`
	ScriptResult     string        `json:"script_result"`
	Evidence         types.JSONMap `json:"evidence"`
	Properties       types.JSONMap `json:"properties"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

func (evidence *Evidence) BeforeCreate(tx *gorm.DB) (err error) {
	if evidence.CreatedBy == uuid.Nil {
		evidence.CreatedBy = *SystemUserID
	}
	return
}

type EvidenceConfig struct {
	ID            uuid.UUID           `json:"id"`
	Lines         []string            `json:"lines"`
	SelectedLines types.JSONStringMap `json:"selected_lines"`
}

type EvidenceConfigAnalysis struct {
	ID uuid.UUID `json:"id"`
}

type EvidenceConfigChange struct {
	ID uuid.UUID `json:"id"`
}

type EvidenceComponent struct {
}

type EvidenceLogs struct {
	Lines []LogLine `json:"lines"`
}

type EvidenceCanaryCheck struct {
}
