package cloudwatch

import (
	"github.com/flanksource/incident-commander/logs"
)

// +kubebuilder:object:generate=true
type Request struct {
	logs.LogsRequestBase `json:",inline" template:"true"`

	// The log group on which to perform the query.
	LogGroup string `json:"logGroup" template:"true"`

	// The query to perform on the log group.
	Query string `json:"query" template:"true"`
}

type Event struct {
	ID      string            `json:"id,omitempty"`
	Time    string            `json:"timestamp,omitempty"`
	Message string            `json:"message,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}
