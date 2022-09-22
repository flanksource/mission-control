package api

import (
	"time"

	"github.com/flanksource/incident-commander/db/types"
	"github.com/google/uuid"
)

type Evidence struct {
	ID               uuid.UUID     `json:"id"`
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
	Evidence         types.JSONMap `json:"evidence"`
	Properties       types.JSONMap `json:"properties"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
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

type LogLine struct {
	Timestamp time.Time           `json:"timestamp"`
	Message   string              `json:"message"`
	Labels    types.JSONStringMap `json:"labels"`
}

type EvidenceCanaryCheck struct {
}
