package loki

import (
	"encoding/json"
	"net/url"
	"strconv"
)

// LokiResponse represents the top-level response from Loki's query_range API.
type LokiResponse struct {
	Status string `json:"status"`
	Data   Data   `json:"data"`
}

// Data holds the actual query results and statistics.
type Data struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
	Stats      map[string]any  `json:"stats"`
}

// Request represents available parameters for Loki queries.
//
// +kubebuilder:object:generate=true
type Request struct {
	// Query is the LogQL query to perform
	Query string `json:"query,omitempty" template:"true"`

	// Limit is the maximum number of lines to return
	Limit *int `json:"limit,omitempty"`

	// Since is a duration used to calculate start relative to end.
	// If end is in the future, start is calculated as this duration before now.
	// Any value specified for start supersedes this parameter.
	Since string `json:"since,omitempty"`

	// Start is the start time of the query. Unix epoch in nanoseconds or supported time format
	Start string `json:"start,omitempty"`

	// End is the end time of the query. Unix epoch in nanoseconds or supported time format
	End string `json:"end,omitempty"`

	// Step is the Query resolution step width in duration format or float number of seconds
	Step string `json:"step,omitempty"`

	// Only return entries at (or greater than) the specified interval, can be a duration format or float number of seconds
	Interval string `json:"interval,omitempty"`

	// Direction is the direction of the query. "forward" or "backward" (default)
	Direction string `json:"direction,omitempty"`
}

// Params returns the URL query parameters for the Loki request
func (r *Request) Params() url.Values {
	// https://grafana.com/docs/loki/latest/reference/loki-http-api/#query-logs-within-a-range-of-time
	params := url.Values{}

	if r.Query != "" {
		params.Set("query", r.Query)
	}
	if r.Limit != nil {
		params.Set("limit", strconv.Itoa(*r.Limit))
	}
	if r.Start != "" {
		params.Set("start", r.Start)
	}
	if r.End != "" {
		params.Set("end", r.End)
	}
	if r.Since != "" {
		params.Set("since", r.Since)
	}
	if r.Step != "" {
		params.Set("step", r.Step)
	}
	if r.Interval != "" {
		params.Set("interval", r.Interval)
	}
	if r.Direction != "" {
		params.Set("direction", r.Direction)
	}

	return params
}
