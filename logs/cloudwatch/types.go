package cloudwatch

import (
	"time"

	"github.com/flanksource/commons/duration"
)

// +kubebuilder:object:generate=true
type Request struct {
	// The log group on which to perform the query.
	LogGroup string `json:"logGroup" template:"true"`

	// The query to perform on the log group.
	Query string `json:"query" template:"true"`

	// The maximum number of log events to return in the query.
	Limit int32 `json:"limit,omitempty"`

	// A RFC3339 timestamp or an age string (e.g. "1h", "2d", "1w")
	Start string `json:"start"`

	// A RFC3339 timestamp or an age string (e.g. "1h", "2d", "1w")
	End string `json:"end,omitempty"`
}

func (p *Request) GetStart() *time.Time {
	if duration, err := duration.ParseDuration(p.Start); err == nil {
		t := time.Now().Add(-time.Duration(duration))
		return &t
	} else if t, err := time.Parse(time.RFC3339, p.Start); err == nil {
		return &t
	}

	return nil
}

func (p *Request) GetEnd() *time.Time {
	if duration, err := duration.ParseDuration(p.End); err == nil {
		t := time.Now().Add(-time.Duration(duration))
		return &t
	} else if t, err := time.Parse(time.RFC3339, p.End); err == nil {
		return &t
	}

	return nil
}

type Event struct {
	ID      string            `json:"id,omitempty"`
	Time    string            `json:"timestamp,omitempty"`
	Message string            `json:"message,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}
