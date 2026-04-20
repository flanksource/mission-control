package events

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	eventHandlerEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "event_handler_events_total",
			Help: "Total number of events processed by event handlers.",
		},
		[]string{"event", "handler", "status"},
	)

	eventHandlerDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "event_handler_duration_seconds",
			Help:    "Duration of event handler invocations in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"event", "handler", "status"},
	)

	eventHandlerLastRunTimestampSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "event_handler_last_run_timestamp_seconds",
			Help: "Unix timestamp of the last event handler invocation.",
		},
		[]string{"event", "handler", "status"},
	)

	eventHandlerInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "event_handler_inflight",
			Help: "Number of in-flight event handler invocations.",
		},
		[]string{"event", "handler"},
	)

	eventHandlerLastStartTimestampSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "event_handler_last_start_timestamp_seconds",
			Help: "Unix timestamp of the last started event handler invocation.",
		},
		[]string{"event", "handler"},
	)
)

func init() {
	prometheus.MustRegister(
		eventHandlerEventsTotal,
		eventHandlerDurationSeconds,
		eventHandlerLastRunTimestampSeconds,
		eventHandlerInFlight,
		eventHandlerLastStartTimestampSeconds,
	)
}

func recordEventHandlerDuration(event, handler string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "fail"
	}

	eventHandlerDurationSeconds.WithLabelValues(event, handler, status).Observe(duration.Seconds())
}

func recordEventHandlerEvents(event, handler string, processed, failed int) {
	if processed > 0 {
		eventHandlerEventsTotal.WithLabelValues(event, handler, "success").Add(float64(processed))
	}
	if failed > 0 {
		eventHandlerEventsTotal.WithLabelValues(event, handler, "failed").Add(float64(failed))
	}
}

func recordEventHandlerLastRun(event, handler string, success bool, at time.Time) {
	status := "success"
	if !success {
		status = "fail"
	}

	eventHandlerLastRunTimestampSeconds.WithLabelValues(event, handler, status).Set(float64(at.Unix()))
}

func recordEventHandlerStart(event, handler string, at time.Time) {
	eventHandlerLastStartTimestampSeconds.WithLabelValues(event, handler).Set(float64(at.Unix()))
	eventHandlerInFlight.WithLabelValues(event, handler).Inc()
}

func recordEventHandlerEnd(event, handler string) {
	eventHandlerInFlight.WithLabelValues(event, handler).Dec()
}
