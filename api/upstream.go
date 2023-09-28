package api

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
)

var TablesToReconcile = []string{
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
