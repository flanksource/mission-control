package loki

import (
	"net/url"
	"strconv"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/logs"
)

// LokiResponse represents the top-level response from Loki's query_range API.
type LokiResponse struct {
	Status    string       `json:"status"`
	Data      Data         `json:"data"`
	ErrorType v1.ErrorType `json:"errorType,omitempty"`
	Error     string       `json:"error,omitempty"`
}

func (t *LokiResponse) ToLogResult(mappingConfig logs.FieldMappingConfig) logs.LogResult {
	output := logs.LogResult{
		Metadata: t.Data.Stats,
	}

	for _, result := range t.Data.Result {
		for _, v := range result.Values {
			if len(v) != 2 {
				continue
			}

			firstObserved, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				logger.Errorf("loki:failed to parse first observed %s: %v", v[0], err)
				continue
			}

			line := logs.LogLine{
				FirstObserved: time.Unix(0, firstObserved),
				Message:       v[1],
				Labels:        result.Stream,
			}

			for k, v := range result.Stream {
				if err := logs.MapFieldToLogLine(k, v, &line, mappingConfig); err != nil {
					// Log or handle mapping error? For now, just log it.
					logger.Errorf("Error mapping field %s for log %s: %v", k, line.ID, err)
				}
			}

			output.Logs = append(output.Logs, line)
		}
	}

	return output
}

type Result struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// Data holds the actual query results and statistics.
type Data struct {
	ResultType string         `json:"resultType"`
	Stats      map[string]any `json:"stats"`

	// Logs per label (aka. stream)
	Result []Result `json:"result"`
}

// Request represents available parameters for Loki queries.
//
// +kubebuilder:object:generate=true
type Request struct {
	logs.LogsRequestBase `json:",inline" template:"true"`

	// Query is the LogQL query to perform
	Query string `json:"query,omitempty" template:"true"`

	// Since is a duration used to calculate start relative to end.
	// If end is in the future, start is calculated as this duration before now.
	// Any value specified for start supersedes this parameter.
	Since string `json:"since,omitempty"`

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
	if r.Limit != "" {
		params.Set("limit", r.Limit)
	}
	if s, err := r.GetStart(); err == nil {
		params.Set("start", s.Format(time.RFC3339))
	}
	if e, err := r.GetEnd(); err == nil {
		params.Set("end", e.Format(time.RFC3339))
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
