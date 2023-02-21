package api

import (
	"time"

	"github.com/flanksource/duty/types"
)

type LogLine struct {
	Timestamp time.Time           `json:"timestamp"`
	Message   string              `json:"message"`
	Labels    types.JSONStringMap `json:"labels"`
}

type LogsResponse struct {
	Total   int       `json:"total"`
	Results []LogLine `json:"results"`
}

type ComponentLogs struct {
	Logs []LogLine `json:"logs"`
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Type string    `json:"type"`
}
