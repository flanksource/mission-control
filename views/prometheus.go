package views

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	promV1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/pkg/clients/prometheus"
)

// executePrometheusQuery executes a Prometheus query and returns results
func executePrometheusQuery(ctx context.Context, pq v1.PrometheusQuery) ([]QueryResultRow, error) {
	client, err := prometheus.NewPrometheusClient(ctx, pq.Connection)
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}

	result, err := executePromQLQuery(ctx, client, pq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute PromQL query: %w", err)
	}

	results, err := transformPrometheusResult(result)
	if err != nil {
		return nil, fmt.Errorf("failed to transform prometheus result: %w", err)
	}

	return results, nil
}

// executePromQLQuery executes a PromQL query against Prometheus
func executePromQLQuery(ctx context.Context, client promV1.API, pq v1.PrometheusQuery) (model.Value, error) {
	result, warnings, err := client.Query(ctx, pq.Query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to execute PromQL query: %w", err)
	}

	if len(warnings) > 0 {
		ctx.Warnf("Prometheus query warnings: %v", warnings)
	}

	return result, nil
}

// transformPrometheusResult transforms Prometheus model.Value to QueryResultRow format
func transformPrometheusResult(result model.Value) ([]QueryResultRow, error) {
	var results []QueryResultRow

	switch v := result.(type) {
	case model.Vector:
		for _, sample := range v {
			row := QueryResultRow{}

			// Add metric labels
			for label, value := range sample.Metric {
				row[string(label)] = string(value)
			}

			// Add the value
			row["value"] = float64(sample.Value)
			results = append(results, row)
		}

	case model.Matrix:
		for _, sampleStream := range v {
			for _, samplePair := range sampleStream.Values {
				row := QueryResultRow{}

				// Add metric labels
				for label, value := range sampleStream.Metric {
					row[string(label)] = string(value)
				}

				// Add timestamp and value
				row["timestamp"] = samplePair.Timestamp.Time().Unix()
				row["value"] = float64(samplePair.Value)
				results = append(results, row)
			}
		}

	case *model.Scalar:
		row := QueryResultRow{
			"value": float64(v.Value),
		}
		results = append(results, row)

	case *model.String:
		row := QueryResultRow{
			"value": v.Value,
		}
		results = append(results, row)

	default:
		return nil, fmt.Errorf("unsupported result type: %T", result)
	}

	return results, nil
}
