package api

import (
	"github.com/flanksource/duty/upstream"
)

var TablesToReconcile = []string{
	"components",
	"config_scrapers",
	"config_items",
	"canaries",
	"checks",
	"topologies",
}

var UpstreamConf upstream.UpstreamConfig
