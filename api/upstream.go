package api

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/google/uuid"
)

var TablesToReconcile = []string{
	"components",
	"config_scrapers",
	"config_items",
	"canaries",
	"checks",
	"check_statuses",
	"topologies",
}

var UpstreamConf upstream.UpstreamConfig

type CanaryPullResponse struct {
	Before   time.Time       `json:"before"`
	Canaries []models.Canary `json:"canaries,omitempty"`
}

type LastPushedConfigResult struct {
	ConfigID   uuid.UUID
	AnalysisID uuid.UUID
	ChangeID   uuid.UUID
}
