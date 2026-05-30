package api

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
)

// List of tables that a mission-control UPSTREAM should allow reconciliation
// from other agents.
var AllowedReconciliationTables = []string{
	"topologies",
	"components",
	"config_scrapers",
	"config_items",
	"canaries",
	"checks",
}

var UpstreamConf upstream.UpstreamConfig

type CanaryPullResponse struct {
	Before   time.Time       `json:"before"`
	Canaries []models.Canary `json:"canaries,omitempty"`
}
