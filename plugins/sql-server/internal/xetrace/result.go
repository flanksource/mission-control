package xetrace

import "time"

// TraceResult is the post-run summary the plugin returns when a trace stops
// (lifted out of pretty.go in the source — clicky-rendered Pretty() omitted
// because the plugin returns JSON to the iframe).
type TraceResult struct {
	SessionName string         `json:"session_name"`
	Database    string         `json:"database"`
	StartedAt   time.Time      `json:"started_at"`
	StoppedAt   time.Time      `json:"stopped_at"`
	Duration    time.Duration  `json:"duration"`
	Events      []Event        `json:"events"`
	Replays     []ReplayResult `json:"replays,omitempty"`
}
