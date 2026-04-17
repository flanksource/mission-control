package events

import (
	"reflect"
	"runtime"
	"strings"
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
)

func init() {
	prometheus.MustRegister(eventHandlerEventsTotal, eventHandlerDurationSeconds)
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

func getHandlerName(fn any) string {
	if fn == nil {
		return "unknown"
	}

	fnValue := reflect.ValueOf(fn)
	if !fnValue.IsValid() || fnValue.Kind() != reflect.Func {
		return "unknown"
	}

	rf := runtime.FuncForPC(fnValue.Pointer())
	if rf == nil {
		return "unknown"
	}

	name := rf.Name()
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return strings.TrimSuffix(name, "-fm")
}
